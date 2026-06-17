#!/bin/sh
set -eu

repo="lexihq/lexi"
install_dir="${INSTALL_DIR:-/usr/local/bin}"
version="${LEXI_VERSION:-latest}"

case "$(uname -s)" in
Linux) os="linux" ;;
*)
	printf 'lexi: unsupported operating system: %s\n' "$(uname -s)" >&2
	exit 1
	;;
esac

case "$(uname -m)" in
x86_64 | amd64) arch="amd64" ;;
aarch64 | arm64) arch="arm64" ;;
*)
	printf 'lexi: unsupported architecture: %s\n' "$(uname -m)" >&2
	exit 1
	;;
esac

asset="lexi-${os}-${arch}"
if [ "$version" = "latest" ]; then
	url="https://github.com/${repo}/releases/latest/download/${asset}"
else
	case "$version" in
	v*) ;;
	*) version="v${version}" ;;
	esac
	url="https://github.com/${repo}/releases/download/${version}/${asset}"
fi

tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT HUP INT TERM

printf 'Downloading %s\n' "$url"
curl -fsSL "$url" -o "$tmp_dir/lexi"

if [ ! -d "$install_dir" ]; then
	if ! mkdir -p "$install_dir" 2>/dev/null; then
		if command -v sudo >/dev/null 2>&1; then
			sudo install -d "$install_dir"
		else
			printf 'lexi: cannot create %s and sudo is unavailable\n' "$install_dir" >&2
			exit 1
		fi
	fi
fi

if [ -w "$install_dir" ]; then
	install -m 0755 "$tmp_dir/lexi" "$install_dir/lexi"
elif command -v sudo >/dev/null 2>&1; then
	sudo install -d "$install_dir"
	sudo install -m 0755 "$tmp_dir/lexi" "$install_dir/lexi"
else
	printf 'lexi: %s is not writable and sudo is unavailable\n' "$install_dir" >&2
	exit 1
fi

printf 'Installed lexi to %s/lexi\n' "$install_dir"
