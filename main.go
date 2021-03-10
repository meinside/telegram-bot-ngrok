// telegram bot for using systemctl remotely
package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	bot "github.com/meinside/telegram-bot-go"
)

const (
	configFilename = "config.json"

	// for monitoring
	defaultMonitorIntervalSeconds = 3

	// for waiting while ngrok launches
	ngrokLaunchDelaySeconds = 5

	// commands
	commandStart         = "/start"
	commandLaunchNgrok   = "/launch"
	commandShutdownNgrok = "/shutdown"
	commandCancel        = "/cancel"

	// messages
	messageDefault                    = "Welcome!"
	messageUnknownCommand             = "Unknown command."
	messageNoTunnels                  = "No tunnels available."
	messageNoConfiguredTunnels        = "No tunnels configured."
	messageWhatToLaunch               = "Which tunnel do you want to launch?"
	messageCancel                     = "Cancel"
	messageCanceled                   = "Canceled."
	messageLaunchedSuccessfullyFormat = "Launched successfully: %s"
	messageLaunchFailed               = "Launch failed."
	messageShutdownSuccessfully       = "Shutdown successfully."
	messageShutdownSuccessfullyFormat = "Shutdown successfully. (%s)"
	messageShutdownFailedFormat       = "Failed to shutdown: %s"

	// api url
	tunnelsAPIURL = "http://localhost:4040/api/tunnels"
)

// struct for config file
type config struct {
	APIToken        string            `json:"api_token"`
	NgrokBinPath    string            `json:"ngrok_bin_path"`
	AvailableIds    []string          `json:"available_ids"`
	MonitorInterval int               `json:"monitor_interval"`
	TunnelParams    map[string]string `json:"tunnel_params"`
	IsVerbose       bool              `json:"is_verbose"`
}

// Read config
func getConfig() (cfg config, err error) {
	var execFilepath string
	if execFilepath, err = os.Executable(); err == nil {
		var file []byte
		if file, err = ioutil.ReadFile(filepath.Join(filepath.Dir(execFilepath), configFilename)); err == nil {
			var conf config
			if err = json.Unmarshal(file, &conf); err == nil {
				return conf, nil
			}
		}
	}

	return config{}, err
}

// variables
var apiToken string
var ngrokBinPath string
var availableIds []string
var monitorInterval int
var tunnelParams map[string]string
var isVerbose bool

// keyboards
var allKeyboards = [][]bot.KeyboardButton{
	bot.NewKeyboardButtons(commandLaunchNgrok, commandShutdownNgrok),
}

// https://ngrok.com/docs/2#client-api
type ngrokTunnels struct {
	Tunnels []ngrokTunnel `json:"tunnels"`
	URI     string        `json:"uri"`
}

type ngrokTunnel struct {
	Name      string                 `json:"name"`
	URI       string                 `json:"uri"`
	PublicURL string                 `json:"public_url"`
	Proto     string                 `json:"proto"`
	Config    map[string]interface{} `json:"config"`
	Metrics   map[string]interface{} `json:"metrics"`
}

var lock sync.Mutex
var cmd *exec.Cmd = nil

var _stdout = log.New(os.Stdout, "", log.LstdFlags)
var _stderr = log.New(os.Stderr, "", log.LstdFlags)

// initialization
func init() {
	// read variables from config file
	if config, err := getConfig(); err == nil {
		apiToken = config.APIToken
		ngrokBinPath = config.NgrokBinPath
		availableIds = config.AvailableIds
		monitorInterval = config.MonitorInterval
		if monitorInterval <= 0 {
			monitorInterval = defaultMonitorIntervalSeconds
		}
		tunnelParams = config.TunnelParams
		isVerbose = config.IsVerbose
	} else {
		panic(err)
	}
}

// check if given Telegram id is available
func isAvailableID(id string) bool {
	for _, v := range availableIds {
		if v == id {
			return true
		}
	}
	return false
}

// get tunnels' status
func tunnelsStatus() (ngrokTunnels, error) {
	var res *http.Response
	var err error

	if res, err = http.Get(tunnelsAPIURL); err == nil {
		defer res.Body.Close()

		var body []byte
		if body, err = ioutil.ReadAll(res.Body); err == nil {
			var tunnels ngrokTunnels
			if err = json.Unmarshal(body, &tunnels); err == nil {
				return tunnels, nil
			}

			if isVerbose {
				_stderr.Printf("failed to parse api response: %s", string(body))
			} else {
				_stderr.Printf("failed to parse api response: %s", err)
			}
		} else {
			_stderr.Printf("failed to read api response: %s", err)
		}
	} else {
		_stderr.Printf("failed to request to api: %s", err)
	}

	return ngrokTunnels{}, err
}

// launch ngrok
func launchNgrok(params ...string) (message string, success bool) {
	lock.Lock()
	defer lock.Unlock()

	if cmd != nil {
		if isVerbose {
			_stdout.Printf("launch: killing process...")
		}

		go func() {
			cmd.Process.Kill()
		}()
		cmd.Wait()
	}
	cmd = exec.Command(ngrokBinPath, params...)

	if isVerbose {
		_stdout.Printf("launch: starting process...")
	}

	var err error
	if err = cmd.Start(); err == nil {
		time.Sleep(ngrokLaunchDelaySeconds * time.Second)

		var tunnels ngrokTunnels
		if tunnels, err = tunnelsStatus(); err == nil {
			status := ""
			for _, tunnel := range tunnels.Tunnels {
				status += fmt.Sprintf("▸ %s\n    %s\n", tunnel.Name, tunnel.PublicURL)
			}
			if len(status) <= 0 {
				status = messageNoTunnels
			}
			return status, true
		}

		return fmt.Sprintf("failed to get tunnels status: %s", err), false
	}

	return fmt.Sprintf("failed to launch: %s", err), false
}

// shutdown ngrok
func shutdownNgrok() (message string, success bool) {
	lock.Lock()
	defer lock.Unlock()

	if cmd == nil {
		return fmt.Sprintf(messageShutdownFailedFormat, "no running process"), false
	}

	if isVerbose {
		_stdout.Printf("shutdown: killing process...")
	}

	go func() {
		cmd.Process.Kill()
	}()

	var msg string
	if err := cmd.Wait(); err == nil {
		msg = messageShutdownSuccessfully
	} else {
		msg = fmt.Sprintf(messageShutdownSuccessfullyFormat, err)
	}
	cmd = nil

	return msg, true
}

// process incoming update from Telegram
func processUpdate(b *bot.Bot, update bot.Update) bool {
	// check username
	var userID string
	if update.Message.From.Username == nil {
		_stderr.Printf("not allowed (no user name): %s", update.Message.From.FirstName)
		return false
	}
	userID = *update.Message.From.Username
	if !isAvailableID(userID) {
		_stderr.Printf("id not allowed: %s\n", userID)

		return false
	}

	// process result
	result := false

	// text from message
	var txt string
	if update.Message.HasText() {
		txt = *update.Message.Text
	} else {
		txt = ""
	}

	var message string
	var options map[string]interface{} = map[string]interface{}{
		"reply_markup": bot.ReplyKeyboardMarkup{
			Keyboard:       allKeyboards,
			ResizeKeyboard: true,
		},
	}

	// 'is typing...'
	b.SendChatAction(update.Message.Chat.ID, bot.ChatActionTyping)

	switch {
	// start
	case strings.HasPrefix(txt, commandStart):
		message = messageDefault
	// launch
	case strings.HasPrefix(txt, commandLaunchNgrok):
		if len(tunnelParams) > 0 {
			// inline keyboards for launching a tunnel
			buttons := [][]bot.InlineKeyboardButton{}
			for k := range tunnelParams {
				data := k
				buttons = append(buttons, []bot.InlineKeyboardButton{
					{
						Text:         k,
						CallbackData: &data,
					},
				})
			}

			// cancel button
			cancel := commandCancel
			buttons = append(buttons, []bot.InlineKeyboardButton{
				{
					Text:         messageCancel,
					CallbackData: &cancel,
				},
			})

			// options
			options["reply_markup"] = bot.InlineKeyboardMarkup{
				InlineKeyboard: buttons,
			}

			message = messageWhatToLaunch
		} else {
			message = messageNoConfiguredTunnels
		}
	case strings.HasPrefix(txt, commandShutdownNgrok):
		message, _ = shutdownNgrok()
	// fallback
	default:
		if len(txt) > 0 {
			message = fmt.Sprintf("%s: %s", txt, messageUnknownCommand)
		} else {
			message = messageUnknownCommand
		}
	}

	// send message
	if sent := b.SendMessage(update.Message.Chat.ID, message, options); sent.Ok {
		result = true
	} else {
		_stderr.Printf("failed to send message: %s", *sent.Description)
	}

	return result
}

// process incoming callback query
func processCallbackQuery(b *bot.Bot, update bot.Update) bool {
	query := *update.CallbackQuery
	txt := *query.Data

	// process result
	result := false
	launchSuccessful := false

	// 'is typing...'
	b.SendChatAction(query.Message.Chat.ID, bot.ChatActionTyping)

	var message string = ""
	if strings.HasPrefix(txt, commandCancel) { // cancel command
		message = ""
	} else {
		if paramStr, exists := tunnelParams[txt]; exists {
			params := strings.Split(paramStr, " ")
			if len(params) > 0 {
				message, launchSuccessful = launchNgrok(params...)
			} else {
				_stderr.Printf("no tunnel parameter")

				return result // == false
			}
		} else {
			_stderr.Printf("unprocessable callback query: %s", txt)

			return result // == false
		}
	}

	// answer callback query
	options := map[string]interface{}{}
	if len(message) > 0 {
		if launchSuccessful {
			options["text"] = fmt.Sprintf(messageLaunchedSuccessfullyFormat, txt)
		} else {
			options["text"] = messageLaunchFailed
		}
	}
	if apiResult := b.AnswerCallbackQuery(query.ID, options); apiResult.Ok {
		// edit message and remove inline keyboards
		options := map[string]interface{}{
			"chat_id":    query.Message.Chat.ID,
			"message_id": query.Message.MessageID,
		}

		if len(message) <= 0 {
			message = messageCanceled
		}
		if apiResult := b.EditMessageText(message, options); apiResult.Ok {
			result = true
		} else {
			_stderr.Printf("failed to edit message text: %s", *apiResult.Description)
		}
	} else {
		_stderr.Printf("failed to answer callback query: %+v", query)
	}

	return result
}

func main() {
	// catch SIGINT and SIGTERM and terminate gracefully
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sig
		os.Exit(1)
	}()

	client := bot.NewClient(apiToken)
	client.Verbose = isVerbose

	// get info about this bot
	if me := client.GetMe(); me.Ok {
		_stdout.Printf("launching bot: @%s (%s)", *me.Result.Username, me.Result.FirstName)

		// delete webhook (getting updates will not work when wehbook is set up)
		if unhooked := client.DeleteWebhook(true); unhooked.Ok {
			// wait for new updates
			client.StartMonitoringUpdates(0, monitorInterval, func(b *bot.Bot, update bot.Update, err error) {
				if err == nil {
					if update.HasMessage() {
						processUpdate(b, update) // process message
					} else if update.HasCallbackQuery() {
						processCallbackQuery(b, update) // process callback query
					}
				} else {
					_stderr.Printf("error while receiving update (%s)", err)
				}
			})
		} else {
			panic("failed to delete webhook")
		}
	} else {
		panic("failed to get info of the bot")
	}
}
