# telegram-bot-ngrok

Telegram bot for launching/terminating ngrok remotely.

Built with golang.

[ngrok](https://ngrok.com/) must be installed & configured first.

## Install and build

```bash
$ git clone https://github.com/meinside/telegram-bot-ngrok.git
$ cd telegram-bot-ngrok/
$ go build
```

## Configure

Create(or copy) a config file:

```bash
$ cp config.json.sample config.json
```

Edit values:

```
{
	"api_token": "0123456789:abcdefghijklmnopqrstuvwxyz",
	"ngrok_bin_path": "/usr/local/bin/ngrok",
	"available_ids": [
		"allowed_telegram_id1",
		"allowed_telegram_id2"
	],
	"monitor_interval": 3,
	"tunnel_params": {
		"HTTP on Port 8080": "http 8888",
		"SSH on Port 22": "tcp 22"
	},
	"is_verbose": false
}
```

## Run

```bash
$ ./telegram-bot-ngrok
```

## Run as a service

Create(or copy) a systemd service file:

```bash
$ sudo cp ./systemd/telegram-bot-ngrok.service /lib/systemd/system/telegram-bot-ngrok.service
sudo vi /lib/systemd/system/telegram-bot-ngrok.service
```

Again, edit values:

```
[Unit]
Description=Telegram Bot for Ngrok
After=syslog.target
After=network.target

[Service]
Type=simple
User=some_user
Group=some_user
WorkingDirectory=/dir/to/telegram-bot-ngrok
ExecStart=/path/to/telegram-bot-ngrok
Restart=always
RestartSec=5
Environment=

[Install]
WantedBy=multi-user.target
```

then,

```bash
$ sudo systemctl enable telegram-bot-ngrok.service
$ sudo systemctl start telegram-bot-ngrok.service
$ sudo systemctl stop telegram-bot-ngrok.service
```

## License

MIT

