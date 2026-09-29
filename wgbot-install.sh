#!/bin/bash
#
# wgbot-install: one-command installer for the wgbot Telegram bot
# (Go binary + VictoriaMetrics single-node) on a server already set up
# with the wireguard-install script (https://github.com/Nyr/wireguard-install).
#
# The bot manages the SAME /etc/wireguard/wg0.conf, fully compatible with
# the reference script: both tools can be used interchangeably.

# Detect Debian users running the script with "sh" instead of bash
if readlink /proc/$$/exe | grep -q "dash"; then
	echo 'This installer needs to be run with "bash", not "sh".'
	exit
fi

# Discard stdin. Needed when running from a one-liner which includes a newline
read -N 999999 -t 0.001

# Detect OS
if grep -qs "ubuntu" /etc/os-release; then
	os="ubuntu"
elif [[ -e /etc/debian_version ]]; then
	os="debian"
elif [[ -e /etc/almalinux-release || -e /etc/rocky-release || -e /etc/centos-release ]]; then
	os="centos"
elif [[ -e /etc/fedora-release ]]; then
	os="fedora"
else
	echo "This installer seems to be running on an unsupported distribution.
Supported distros are Ubuntu, Debian, AlmaLinux, Rocky Linux, CentOS and Fedora."
	exit
fi

# Detect environments where $PATH does not include the sbin directories
if ! grep -q sbin <<< "$PATH"; then
	echo '$PATH does not include sbin. Try using "su -" instead of "su".'
	exit
fi

if [[ "$EUID" -ne 0 ]]; then
	echo "This installer needs to be run with superuser privileges."
	exit
fi

# The bot requires an existing wireguard-install setup
if [[ ! -e /etc/wireguard/wg0.conf ]] || ! hash wg 2>/dev/null; then
	echo "WireGuard is not installed on this system.
Run wireguard-install.sh first (https://github.com/Nyr/wireguard-install),
then run this installer again to add the management bot."
	exit
fi

# Store the absolute path of the directory where the script is located
script_dir="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"

install_vm () {
	# Figure out the latest VictoriaMetrics release for this architecture
	arch=$(uname -m)
	case "$arch" in
		x86_64) vm_arch="amd64" ;;
		aarch64|arm64) vm_arch="arm64" ;;
		*)
			echo "Unsupported architecture: $arch"
			exit
		;;
	esac
	vm_latest=$(wget -qO- -T 10 -t 1 "https://api.github.com/repos/VictoriaMetrics/VictoriaMetrics/releases/latest" 2>/dev/null || curl -m 15 -sL "https://api.github.com/repos/VictoriaMetrics/VictoriaMetrics/releases/latest")
	vm_version=$(grep -m 1 -oE '"tag_name": *"[^"]+"' <<< "$vm_latest" | cut -d '"' -f 4)
	if [[ -z "$vm_version" ]]; then
		vm_version="v1.102.0"
		echo "Could not detect the latest release, falling back to $vm_version."
	fi
	vm_url="https://github.com/VictoriaMetrics/VictoriaMetrics/releases/download/${vm_version}/victoria-metrics-linux-${vm_arch}-${vm_version}.tar.gz"
	echo "Downloading VictoriaMetrics ${vm_version} (${vm_arch})..."
	{ wget -qO- "$vm_url" 2>/dev/null || curl -sL "$vm_url" ; } | tar xz -C /usr/local/bin/ victoria-metrics-prod
	if [[ ! -e /usr/local/bin/victoria-metrics-prod ]]; then
		echo "Failed to download VictoriaMetrics."
		exit
	fi
	mv -f /usr/local/bin/victoria-metrics-prod /usr/local/bin/victoria-metrics
	mkdir -p /var/lib/victoria-metrics
	sed -e "s/__VMPORT__/$vm_port/" -e "s/__RETENTION__/$vm_retention/" \
		"$script_dir/wgbot/deploy/victoriametrics.service" > /etc/systemd/system/victoriametrics.service
	systemctl daemon-reload
	systemctl enable --now victoriametrics.service
}

build_wgbot () {
	# Install Go if it is not available
	if ! hash go 2>/dev/null; then
		echo "Installing the Go toolchain..."
		if [[ "$os" == "ubuntu" || "$os" == "debian" ]]; then
			apt-get update
			apt-get install -y golang-go
		else
			dnf install -y golang
		fi
	fi
	if ! hash go 2>/dev/null; then
		echo "Go could not be installed. Install Go manually and re-run."
		exit
	fi
	echo "Building wgbot..."
	if ! (cd "$script_dir/wgbot" && CGO_ENABLED=0 go build -o /usr/local/bin/wgbot .); then
		echo "wgbot build failed."
		exit
	fi
}

configure_wgbot () {
	echo
	echo "wgbot configuration"
	echo "-------------------"
	until [[ -n "$token" ]]; do
		read -p "Telegram bot token (from @BotFather): " token
	done
	until [[ "$admin_ids" =~ ^[0-9]+(,[0-9]+)*$ ]]; do
		read -p "Admin Telegram user IDs, comma-separated (get yours from @userinfobot): " admin_ids
	done
	read -p "Chat ID for the daily summary [first admin]: " summary_chat_id
	read -p "Daily summary time (HH:MM) [09:00]: " summary_time
	[[ -z "$summary_time" ]] && summary_time="09:00"
	[[ -z "$summary_chat_id" ]] && summary_chat_id=$(cut -d ',' -f 1 <<< "$admin_ids")
	read -p "VictoriaMetrics port [8428]: " vm_port
	[[ -z "$vm_port" ]] && vm_port="8428"
	read -p "Metrics retention period [90d]: " vm_retention
	[[ -z "$vm_retention" ]] && vm_retention="90d"

	mkdir -p /etc/wgbot /etc/wireguard/clients
	chmod 700 /etc/wireguard/clients
	umask 177
	cat << EOF > /etc/wgbot/wgbot.env
WGBOT_TOKEN=$token
WGBOT_ADMIN_IDS=$admin_ids
WGBOT_SUMMARY_CHAT_ID=$summary_chat_id
WGBOT_SUMMARY_TIME=$summary_time
WGBOT_VM_URL=http://127.0.0.1:$vm_port
WGBOT_WG_CONF=/etc/wireguard/wg0.conf
WGBOT_WG_IFACE=wg0
WGBOT_CLIENTS_DIR=/etc/wireguard/clients
EOF
	umask 022
	cp "$script_dir/wgbot/deploy/wgbot.service" /etc/systemd/system/wgbot.service
}

if [[ ! -e /etc/wgbot/wgbot.env ]]; then
	clear
	echo 'Welcome to the wgbot installer!'
	echo 'It will set up the Telegram management bot for your existing'
	echo 'WireGuard installation, plus VictoriaMetrics for traffic statistics.'
	echo
	token=""
	admin_ids=""
	configure_wgbot
	install_vm
	build_wgbot
	systemctl daemon-reload
	systemctl enable --now wgbot.service
	echo
	echo "Finished!"
	echo
	echo "Talk to your bot in Telegram: /help shows the available commands."
	echo "Daily summary is sent daily at $summary_time."
	echo "Metrics: http://127.0.0.1:$vm_port (VictoriaMetrics, localhost only)."
else
	clear
	echo "wgbot is already installed."
	echo
	echo "Select an option:"
	echo "   1) Reconfigure (token, admins, summary time, VM settings)"
	echo "   2) Restart wgbot"
	echo "   3) Rebuild wgbot from sources and restart"
	echo "   4) Remove wgbot and VictoriaMetrics (WireGuard is NOT touched)"
	echo "   5) Exit"
	read -p "Option: " option
	until [[ "$option" =~ ^[1-5]$ ]]; do
		echo "$option: invalid selection."
		read -p "Option: " option
	done
	case "$option" in
		1)
			# derive current VM settings as defaults, then re-ask everything
			vm_port=$(grep -oE 'WGBOT_VM_URL=http://127.0.0.1:[0-9]+' /etc/wgbot/wgbot.env | grep -oE '[0-9]+$')
			vm_retention=$(grep -oE 'retentionPeriod=[^ "]+' /etc/systemd/system/victoriametrics.service 2>/dev/null | cut -d '=' -f 2)
			[[ -z "$vm_port" ]] && vm_port="8428"
			[[ -z "$vm_retention" ]] && vm_retention="90d"
			token=""
			admin_ids=""
			configure_wgbot
			# regenerate the VM unit in case port/retention changed
			sed -e "s/__VMPORT__/$vm_port/" -e "s/__RETENTION__/$vm_retention/" \
				"$script_dir/wgbot/deploy/victoriametrics.service" > /etc/systemd/system/victoriametrics.service
			systemctl daemon-reload
			systemctl restart victoriametrics.service
			systemctl restart wgbot.service
			echo "Reconfigured and restarted."
		;;
		2)
			systemctl restart wgbot.service
			echo "wgbot restarted."
		;;
		3)
			build_wgbot
			systemctl restart wgbot.service
			echo "wgbot rebuilt and restarted."
		;;
		4)
			read -p "Confirm removal of wgbot and VictoriaMetrics? [y/N]: " remove
			until [[ "$remove" =~ ^[yYnN]*$ ]]; do
				echo "$remove: invalid selection."
				read -p "Confirm removal of wgbot and VictoriaMetrics? [y/N]: " remove
			done
			if [[ "$remove" =~ ^[yY]$ ]]; then
				systemctl disable --now wgbot.service victoriametrics.service
				rm -f /etc/systemd/system/wgbot.service /etc/systemd/system/victoriametrics.service
				rm -rf /etc/wgbot /var/lib/victoria-metrics
				rm -f /usr/local/bin/wgbot /usr/local/bin/victoria-metrics
				systemctl daemon-reload
				echo "wgbot and VictoriaMetrics removed. WireGuard configuration is untouched."
			else
				echo "Removal aborted!"
			fi
		;;
		5)
			exit
		;;
	esac
fi
