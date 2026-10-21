#!/bin/sh
# Installer download helper, prepended to the install script of every agent
# whose documented installer is a curl one-liner piped into a shell. Piped,
# the pipeline's status is the shell's, and a shell reading an empty script
# exits 0, so a download the egress policy refuses would be recorded as a
# successful install. This only defines a function; the caller runs
#
#   agentctl_fetch_run <agent> <shell> <url> [curl-flags...]
#
# which downloads <url> to a temporary file with the agent's own curl flags,
# fails with one "agentctl: <agent>: ..." line when curl does, and otherwise
# runs the file with <shell> and returns its status. The file is removed
# either way. POSIX sh: Incus guests may be Alpine.

agentctl_fetch_run() {
	agentctl_fetch_agent=$1
	agentctl_fetch_shell=$2
	agentctl_fetch_url=$3
	shift 3
	agentctl_fetch_file=$(mktemp) || {
		echo "agentctl: $agentctl_fetch_agent: cannot create a temporary file for its installer" >&2
		return 1
	}
	curl "$@" -o "$agentctl_fetch_file" "$agentctl_fetch_url"
	agentctl_fetch_status=$?
	if [ "$agentctl_fetch_status" -ne 0 ]; then
		rm -f "$agentctl_fetch_file"
		echo "agentctl: $agentctl_fetch_agent: could not download $agentctl_fetch_url (curl exit $agentctl_fetch_status); the sandbox's egress policy may be refusing it — check the agent's allow preset" >&2
		return "$agentctl_fetch_status"
	fi
	"$agentctl_fetch_shell" "$agentctl_fetch_file"
	agentctl_fetch_status=$?
	rm -f "$agentctl_fetch_file"
	return "$agentctl_fetch_status"
}
