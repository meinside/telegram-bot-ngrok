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
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	bot "github.com/meinside/telegram-bot-go"
)

const (
	ConfigFilename = "config.json"

	// for monitoring
	DefaultMonitorIntervalSeconds = 3

	// for waiting while ngrok launches
	NgrokLaunchDelaySeconds = 5

	// commands
	CommandStart         = "/start"
	CommandLaunchNgrok   = "/launch"
	CommandShutdownNgrok = "/shutdown"
	CommandCancel        = "/cancel"

	// messages
	MessageDefault                    = "Welcome"
	MessageUnknownCommand             = "Unknown command"
	MessageNoTunnels                  = "No tunnels available"
	MessageNoConfiguredTunnels        = "No tunnes configured"
	MessageWhatToLaunch               = "Choose to launch"
	MessageCancel                     = "Cancel"
	MessageCanceled                   = "Canceled"
	MessageLaunchedSuccessfullyFormat = "Launched successfully: %s"
	MessageLaunchFailed               = "Launch failed"
	MessageShutdownSuccessfully       = "Shutdown successfully"
	MessageShutdownSuccessfullyFormat = "Shutdown successfully: %s"
	MessageShutdownFailedFormat       = "Failed to shutdown: %s"

	// api url
	TunnelsApiUrl = "http://localhost:4040/api/tunnels"
)

// struct for config file
type Config struct {
	ApiToken        string            `json:"api_token"`
	NgrokBinPath    string            `json:"ngrok_bin_path"`
	AvailableIds    []string          `json:"available_ids"`
	MonitorInterval int               `json:"monitor_interval"`
	TunnelParams    map[string]string `json:"tunnel_params"`
	IsVerbose       bool              `json:"is_verbose"`
}

// Read config
func getConfig() (config Config, err error) {
	_, filename, _, _ := runtime.Caller(0) // = __FILE__

	if file, err := ioutil.ReadFile(filepath.Join(path.Dir(filename), ConfigFilename)); err == nil {
		var conf Config
		if err := json.Unmarshal(file, &conf); err == nil {
			return conf, nil
		} else {
			return Config{}, err
		}
	} else {
		return Config{}, err
	}
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
	bot.NewKeyboardButtons(CommandLaunchNgrok, CommandShutdownNgrok),
}

// https://ngrok.com/docs/2#client-api
type NgrokTunnels struct {
	Tunnels []NgrokTunnel `json:"tunnels"`
	Uri     string        `json:"uri"`
}

type NgrokTunnel struct {
	Name      string                 `json:"name"`
	Uri       string                 `json:"uri"`
	PublicUrl string                 `json:"public_url"`
	Proto     string                 `json:"proto"`
	Config    map[string]interface{} `json:"config"`
	Metrics   map[string]interface{} `json:"metrics"`
}

var lock sync.Mutex
var cmd *exec.Cmd = nil

// initialization
func init() {
	// read variables from config file
	if config, err := getConfig(); err == nil {
		apiToken = config.ApiToken
		ngrokBinPath = config.NgrokBinPath
		availableIds = config.AvailableIds
		monitorInterval = config.MonitorInterval
		if monitorInterval <= 0 {
			monitorInterval = DefaultMonitorIntervalSeconds
		}
		tunnelParams = config.TunnelParams
		isVerbose = config.IsVerbose
	} else {
		panic(err)
	}
}

// check if given Telegram id is available
func isAvailableId(id string) bool {
	for _, v := range availableIds {
		if v == id {
			return true
		}
	}
	return false
}

// get tunnels' status
func tunnelsStatus() (NgrokTunnels, error) {
	var res *http.Response
	var err error

	if res, err = http.Get(TunnelsApiUrl); err == nil {
		defer res.Body.Close()

		var body []byte
		if body, err = ioutil.ReadAll(res.Body); err == nil {
			var tunnels NgrokTunnels
			if err = json.Unmarshal(body, &tunnels); err == nil {
				return tunnels, nil
			} else {
				if isVerbose {
					log.Printf("*** Failed to parse api response: %s\n", string(body))
				} else {
					log.Printf("*** Failed to parse api response: %s\n", err)
				}
			}
		} else {
			log.Printf("*** Failed to read api response: %s\n", err)
		}
	} else {
		log.Printf("*** Failed to request to api: %s\n", err)
	}

	return NgrokTunnels{}, err
}

// launch ngrok
func launchNgrok(params ...string) (message string, success bool) {
	lock.Lock()
	defer lock.Unlock()

	if cmd != nil {
		if isVerbose {
			log.Printf("launch: killing process...")
		}

		go func() {
			cmd.Process.Kill()
		}()
		cmd.Wait()
	}
	cmd = exec.Command(ngrokBinPath, params...)

	if isVerbose {
		log.Printf("launch: starting process...")
	}

	if err := cmd.Start(); err == nil {
		time.Sleep(NgrokLaunchDelaySeconds * time.Second)

		if tunnels, err := tunnelsStatus(); err == nil {
			status := ""
			for _, tunnel := range tunnels.Tunnels {
				status += fmt.Sprintf("▸ %s: %s\n", tunnel.Name, tunnel.PublicUrl)
			}
			if len(status) <= 0 {
				status = MessageNoTunnels
			}
			return status, true
		} else {
			return fmt.Sprintf("Failed to get tunnels status: %s", err), false
		}
	} else {
		return fmt.Sprintf("Failed to launch: %s", err), false
	}
}

// shutdown ngrok
func shutdownNgrok() (message string, success bool) {
	lock.Lock()
	defer lock.Unlock()

	if cmd == nil {
		return fmt.Sprintf(MessageShutdownFailedFormat, "no running process"), false
	} else {

		if isVerbose {
			log.Printf("shutdown: killing process...")
		}

		go func() {
			cmd.Process.Kill()
		}()

		var msg string
		if err := cmd.Wait(); err == nil {
			msg = MessageShutdownSuccessfully
		} else {
			msg = fmt.Sprintf(MessageShutdownSuccessfullyFormat, err)
		}
		cmd = nil

		return msg, true
	}
}

// process incoming update from Telegram
func processUpdate(b *bot.Bot, update bot.Update) bool {
	// check username
	var userId string
	if update.Message.From.Username == nil {
		log.Printf("*** Not allowed (no user name): %s\n", *update.Message.From.FirstName)
		return false
	}
	userId = *update.Message.From.Username
	if !isAvailableId(userId) {
		log.Printf("*** Id not allowed: %s\n", userId)

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
		//"parse_mode": bot.ParseModeMarkdown,
	}

	// 'is typing...'
	b.SendChatAction(update.Message.Chat.Id, bot.ChatActionTyping)

	switch {
	// start
	case strings.HasPrefix(txt, CommandStart):
		message = MessageDefault
	// launch
	case strings.HasPrefix(txt, CommandLaunchNgrok):
		if len(tunnelParams) > 0 {
			// inline keyboards for launching a tunnel
			buttons := [][]bot.InlineKeyboardButton{}
			for k, _ := range tunnelParams {
				buttons = append(buttons, []bot.InlineKeyboardButton{
					bot.InlineKeyboardButton{
						Text:         k,
						CallbackData: k,
					},
				})
			}
			buttons = append(buttons, []bot.InlineKeyboardButton{
				bot.InlineKeyboardButton{
					Text:         MessageCancel,
					CallbackData: CommandCancel,
				},
			})
			options["reply_markup"] = bot.InlineKeyboardMarkup{
				InlineKeyboard: buttons,
			}
			message = MessageWhatToLaunch
		} else {
			message = MessageNoConfiguredTunnels
		}
	case strings.HasPrefix(txt, CommandShutdownNgrok):
		message, _ = shutdownNgrok()
	// fallback
	default:
		message = fmt.Sprintf("%s: %s", txt, MessageUnknownCommand)
	}

	// send message
	if sent := b.SendMessage(update.Message.Chat.Id, &message, options); sent.Ok {
		result = true
	} else {
		log.Printf("*** Failed to send message: %s\n", *sent.Description)
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
	b.SendChatAction(query.Message.Chat.Id, bot.ChatActionTyping)

	var message string = ""
	if strings.HasPrefix(txt, CommandCancel) { // cancel command
		message = ""
	} else {
		if paramStr, exists := tunnelParams[txt]; exists {
			params := strings.Split(paramStr, " ")
			if len(params) > 0 {
				message, launchSuccessful = launchNgrok(params...)
			} else {
				log.Printf("*** No tunnel parameter\n")

				return result // == false
			}
		} else {
			log.Printf("*** Unprocessable callback query: %s\n", txt)

			return result // == false
		}
	}

	// answer callback query
	options := map[string]interface{}{}
	if len(message) > 0 {
		if launchSuccessful {
			options["text"] = fmt.Sprintf(MessageLaunchedSuccessfullyFormat, txt)
		} else {
			options["text"] = MessageLaunchFailed
		}
	}
	if apiResult := b.AnswerCallbackQuery(query.Id, options); apiResult.Ok {
		// edit message and remove inline keyboards
		options := map[string]interface{}{
			"chat_id":    query.Message.Chat.Id,
			"message_id": query.Message.MessageId,
		}

		if len(message) <= 0 {
			message = MessageCanceled
		}
		if apiResult := b.EditMessageText(&message, options); apiResult.Ok {
			result = true
		} else {
			log.Printf("*** Failed to edit message text: %s\n", *apiResult.Description)
		}
	} else {
		log.Printf("*** Failed to answer callback query: %+v\n", query)
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
		log.Printf("Launching bot: @%s (%s)\n", *me.Result.Username, *me.Result.FirstName)

		// delete webhook (getting updates will not work when wehbook is set up)
		if unhooked := client.DeleteWebhook(); unhooked.Ok {
			// wait for new updates
			client.StartMonitoringUpdates(0, monitorInterval, func(b *bot.Bot, update bot.Update, err error) {
				if err == nil {
					if update.HasMessage() {
						// process message
						processUpdate(b, update)
					} else if update.HasCallbackQuery() {
						// process callback query
						processCallbackQuery(b, update)
					}
				} else {
					log.Printf("*** Error while receiving update (%s)\n", err)
				}
			})
		} else {
			panic("Failed to delete webhook")
		}
	} else {
		panic("Failed to get info of the bot")
	}
}
