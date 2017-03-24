# telegram-bot-ngrok

Telegram bot for launching/terminating ngrok remotely.

Built with golang.

[ngrok](https://ngrok.com/) must be installed & configured first.

## Install and build

```bash
$ go get -d github.com/meinside/telegram-bot-ngrok
$ cd $GOPATH/src/github.com/meinside/telegram-bot-ngrok
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

![screen shot 2016-12-08 at 12 04 59](https://cloud.githubusercontent.com/assets/185988/20996144/ce7dd0ea-bd3e-11e6-9e30-0f2a9e724276.png)

![screen shot 2016-12-08 at 12 05 26](https://cloud.githubusercontent.com/assets/185988/20996147/d0ea23e2-bd3e-11e6-8307-98ca07c59969.png)

## Run as a service

Create(or copy) a systemd service file:

```bash
$ sudo cp ./systemd/telegram-bot-ngrok.service /lib/systemd/system/telegram-bot-ngrok.service
$ sudo vi /lib/systemd/system/telegram-bot-ngrok.service
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

