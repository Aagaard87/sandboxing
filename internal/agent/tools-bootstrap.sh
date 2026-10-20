#!/bin/sh
# ripgrep and fd bootstrap, prepended to pi's install script next to the
# Node one. pi downloads both from github.com at first use when they are
# not on PATH, which the instance's egress policy refuses; installing them
# from the distribution avoids that when its package source is allowed. It
# only defines functions; the caller runs
#
#   agentctl_ensure_tools <agent>
#
# which never fails: without the tools pi still installs, it just cannot
# use them. POSIX sh: Incus guests may be Alpine.

# Where the fd symlink to Debian's fdfind goes; a variable so tests can
# point it somewhere writable.
agentctl_tools_bindir=/usr/local/bin

# agentctl_tools_pkgmgr prints apt, apk or dnf, by which binary exists, in
# that order, or nothing for any other guest.
agentctl_tools_pkgmgr() {
	if command -v apt-get >/dev/null 2>&1; then
		echo apt
	elif command -v apk >/dev/null 2>&1; then
		echo apk
	elif command -v dnf >/dev/null 2>&1; then
		echo dnf
	fi
}

agentctl_tools_unavailable() {
	echo "agentctl: $1: could not install ripgrep and fd ($2); create the instance with --allow-preset apt (Debian/Ubuntu) or apk (Alpine) so agentctl can install them, otherwise $1 will try to download them itself and the egress policy will refuse that" >&2
}

agentctl_ensure_tools() {
	if command -v rg >/dev/null 2>&1 && command -v fd >/dev/null 2>&1; then
		return 0
	fi
	if ! sudo -n true 2>/dev/null; then
		echo "agentctl: $1: not installing ripgrep and fd (no passwordless sudo); $1 will try to download them itself and the egress policy will refuse that" >&2
		return 0
	fi
	case $(agentctl_tools_pkgmgr) in
	apt)
		# Debian's lists may be missing or stale on a fresh image, and a
		# postinst prompt would hang with no terminal to answer it.
		{ sudo -n apt-get update -qq &&
			sudo -n env DEBIAN_FRONTEND=noninteractive apt-get install -y -qq ripgrep fd-find; } ||
			{
				agentctl_tools_unavailable "$1" "apt-get exited $?"
				return 0
			}
		# Debian installs fd as fdfind.
		if ! command -v fd >/dev/null 2>&1 && command -v fdfind >/dev/null 2>&1; then
			sudo -n ln -s "$(command -v fdfind)" "$agentctl_tools_bindir/fd" ||
				agentctl_tools_unavailable "$1" "linking $agentctl_tools_bindir/fd to fdfind failed"
		fi
		;;
	apk)
		sudo -n apk add --no-cache ripgrep fd ||
			agentctl_tools_unavailable "$1" "apk add exited $?"
		;;
	dnf)
		sudo -n dnf install -y ripgrep fd-find ||
			agentctl_tools_unavailable "$1" "dnf install exited $?"
		;;
	*)
		agentctl_tools_unavailable "$1" "no apt-get, apk or dnf in the guest"
		;;
	esac
	return 0
}
