#!/usr/bin/env bash

# Adapted from https://github.com/railwayapp/nixpacks/blob/main/install.sh

help_text="Options

   -V, --verbose
   Enable verbose output for the installer

   -f, -y, --force, --yes
   Skip the confirmation prompt during installation

   -p, --platform
   Override the platform identified by the installer

   -b, --bin-dir
   Override the bin installation directory

   -a, --arch
   Override the architecture identified by the installer

   -B, --base-url
   Override the base URL used for downloading releases

   -v, --version
   Install a specific version (e.g., v0.0.1)

   -r, --remove
   Uninstall coolpack

   -h, --help
   Get some help

"

set -eu
printf '\n'

BOLD="$(tput bold 2>/dev/null || printf '')"
GREY="$(tput setaf 0 2>/dev/null || printf '')"
UNDERLINE="$(tput smul 2>/dev/null || printf '')"
RED="$(tput setaf 1 2>/dev/null || printf '')"
GREEN="$(tput setaf 2 2>/dev/null || printf '')"
YELLOW="$(tput setaf 3 2>/dev/null || printf '')"
BLUE="$(tput setaf 4 2>/dev/null || printf '')"
MAGENTA="$(tput setaf 5 2>/dev/null || printf '')"
NO_COLOR="$(tput sgr0 2>/dev/null || printf '')"

SUPPORTED_TARGETS="linux_amd64 linux_arm64 darwin_arm64"

info() {
  printf '%s\n' "${BOLD}${GREY}>${NO_COLOR} $*"
}

debug() {
  if [[ -n "${VERBOSE-}" ]]; then
    printf '%s\n' "${BOLD}${GREY}>${NO_COLOR} $*"
  fi
}

warn() {
  printf '%s\n' "${YELLOW}! $*${NO_COLOR}"
}

error() {
  printf '%s\n' "${RED}x $*${NO_COLOR}" >&2
}

completed() {
  printf '%s\n' "${GREEN}✓${NO_COLOR} $*"
}

has() {
  command -v "$1" 1>/dev/null 2>&1
}

get_tmpfile() {
  local suffix
  suffix="$1"
  if has mktemp; then
    printf "%s%s.%s.%s" "$(mktemp)" "-coolpack" "${RANDOM}" "${suffix}"
  else
    printf "/tmp/coolpack.%s" "${suffix}"
  fi
}

test_writeable() {
  local path
  path="${1:-}/test.txt"
  if touch "${path}" 2>/dev/null; then
    rm "${path}"
    return 0
  else
    return 1
  fi
}

download() {
  file="$1"
  url="$2"
  touch "$file"

  if has curl; then
    cmd="curl --fail --silent --location --output $file $url"
  elif has wget; then
    cmd="wget --quiet --output-document=$file $url"
  elif has fetch; then
    cmd="fetch --quiet --output=$file $url"
  else
    error "No HTTP download program (curl, wget, fetch) found, exiting…"
    return 1
  fi

  $cmd && return 0 || rc=$?

  error "Command failed (exit code $rc): ${BLUE}${cmd}${NO_COLOR}"
  printf "\n" >&2
  info "This is likely due to coolpack not yet supporting your configuration."
  info "If you would like to see a build for your configuration,"
  info "please create an issue requesting a build for ${MAGENTA}${TARGET}${NO_COLOR}:"
  info "${BOLD}${UNDERLINE}https://github.com/coollabsio/coolpack/issues/new/${NO_COLOR}"
  return $rc
}

unpack() {
  local archive=$1
  local bin_dir=$2
  local sudo=${3-}

  case "$archive" in
    *.tar.gz)
      ${sudo} tar -xzf "${archive}" -C "${bin_dir}"
      return 0
      ;;
  esac

  error "Unknown package extension."
  printf "\n"
  info "This almost certainly results from a bug in this script--please file a"
  info "bug report at https://github.com/coollabsio/coolpack/issues"
  return 1
}

elevate_priv() {
  if ! has sudo; then
    error 'Could not find the command "sudo", needed to get permissions for install.'
    info "Please run this script as root, or install sudo."
    exit 1
  fi
  if ! sudo -v; then
    error "Superuser not granted, aborting installation"
    exit 1
  fi
}

install() {
  local msg
  local sudo
  local archive
  local ext="$1"

  if test_writeable "${BIN_DIR}"; then
    sudo=""
    msg="Installing coolpack, please wait…"
  else
    warn "Escalated permissions are required to install to ${BIN_DIR}"
    elevate_priv
    sudo="sudo"
    msg="Installing coolpack as root, please wait…"
  fi
  info "$msg"

  archive=$(get_tmpfile "$ext")

  # download to the temp file
  download "${archive}" "${URL}"

  # unpack the temp file to the bin dir, using sudo if required
  unpack "${archive}" "${BIN_DIR}" "${sudo}"

  # rename binary to coolpack
  if [ -f "${BIN_DIR}/coolpack_${PLATFORM}_${ARCH}" ]; then
    ${sudo} mv "${BIN_DIR}/coolpack_${PLATFORM}_${ARCH}" "${BIN_DIR}/coolpack"
  fi

  # remove quarantine attribute on macOS
  if [ "${PLATFORM}" = "darwin" ] && has xattr; then
    ${sudo} xattr -d com.apple.quarantine "${BIN_DIR}/coolpack" 2>/dev/null || true
  fi
}

detect_platform() {
  local platform
  platform="$(uname -s | tr '[:upper:]' '[:lower:]')"

  case "${platform}" in
    linux) platform="linux" ;;
    darwin) platform="darwin" ;;
    *)
      error "Unsupported platform: ${platform}"
      exit 1
      ;;
  esac

  printf '%s' "${platform}"
}

detect_arch() {
  local arch
  arch="$(uname -m | tr '[:upper:]' '[:lower:]')"

  case "${arch}" in
    x86_64 | amd64) arch="amd64" ;;
    aarch64 | arm64) arch="arm64" ;;
    *)
      error "Unsupported architecture: ${arch}"
      exit 1
      ;;
  esac

  printf '%s' "${arch}"
}

detect_target() {
  local arch="$1"
  local platform="$2"
  local target="${platform}_${arch}"

  printf '%s' "${target}"
}

confirm() {
  if [ -t 0 ]; then
    if [ -z "${FORCE-}" ]; then
      printf "%s " "${MAGENTA}?${NO_COLOR} $* ${BOLD}[y/N]${NO_COLOR}"
      set +e
      read -r yn </dev/tty
      rc=$?
      set -e
      if [ $rc -ne 0 ]; then
        error "Error reading from prompt (please re-run with the '--yes' option)"
        exit 1
      fi
      if [ "$yn" != "y" ] && [ "$yn" != "yes" ]; then
        error 'Aborting (please answer "yes" to continue)'
        exit 1
      fi
    fi
  fi
}

check_bin_dir() {
  local bin_dir="$1"

  if [ ! -d "$BIN_DIR" ]; then
    error "Installation location $BIN_DIR does not appear to be a directory"
    info "Make sure the location exists and is a directory, then try again."
    exit 1
  fi

  local good
  good=$(
    IFS=:
    for path in $PATH; do
      if [ "${path}" = "${bin_dir}" ]; then
        printf 1
        break
      fi
    done
  )

  if [ "${good}" != "1" ]; then
    warn "Bin directory ${bin_dir} is not in your \$PATH"
  fi
}

is_build_available() {
  local arch="$1"
  local platform="$2"
  local target="$3"

  local good

  good=$(
    IFS=" "
    for t in $SUPPORTED_TARGETS; do
      if [ "${t}" = "${target}" ]; then
        printf 1
        break
      fi
    done
  )

  if [ "${good}" != "1" ]; then
    error "${arch} builds for ${platform} are not yet available for coolpack"
    printf "\n" >&2
    info "If you would like to see a build for your configuration,"
    info "please create an issue requesting a build for ${MAGENTA}${target}${NO_COLOR}:"
    info "${BOLD}${UNDERLINE}https://github.com/coollabsio/coolpack/issues/new/${NO_COLOR}"
    printf "\n"
    exit 1
  fi
}

get_latest_version() {
  local url="https://api.github.com/repos/coollabsio/coolpack/releases/latest"
  local version

  if has curl; then
    version=$(curl -fsSL "$url" | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/')
  elif has wget; then
    version=$(wget -qO- "$url" | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/')
  else
    error "No HTTP download program (curl, wget) found, exiting…"
    exit 1
  fi

  if [ -z "$version" ]; then
    error "Could not determine latest version"
    exit 1
  fi

  printf '%s' "$version"
}

UNINSTALL=0
HELP=0

# defaults
if [ -z "${COOLPACK_VERSION-}" ]; then
  COOLPACK_VERSION=""
fi

if [ -z "${COOLPACK_PLATFORM-}" ]; then
  PLATFORM="$(detect_platform)"
fi

if [ -z "${COOLPACK_BIN_DIR-}" ]; then
  BIN_DIR=/usr/local/bin
fi

if [ -z "${COOLPACK_ARCH-}" ]; then
  ARCH="$(detect_arch)"
fi

if [ -z "${COOLPACK_BASE_URL-}" ]; then
  BASE_URL="https://github.com/coollabsio/coolpack/releases"
fi

# parse argv variables
while [ "$#" -gt 0 ]; do
  case "$1" in
  -p | --platform)
    PLATFORM="$2"
    shift 2
    ;;
  -b | --bin-dir)
    BIN_DIR="$2"
    shift 2
    ;;
  -a | --arch)
    ARCH="$2"
    shift 2
    ;;
  -B | --base-url)
    BASE_URL="$2"
    shift 2
    ;;
  -v | --version)
    COOLPACK_VERSION="$2"
    shift 2
    ;;
  -V | --verbose)
    VERBOSE=1
    shift 1
    ;;
  -f | -y | --force | --yes)
    FORCE=1
    shift 1
    ;;
  -r | --remove | --uninstall)
    UNINSTALL=1
    shift 1
    ;;
  -h | --help)
    HELP=1
    shift 1
    ;;
  -p=* | --platform=*)
    PLATFORM="${1#*=}"
    shift 1
    ;;
  -b=* | --bin-dir=*)
    BIN_DIR="${1#*=}"
    shift 1
    ;;
  -a=* | --arch=*)
    ARCH="${1#*=}"
    shift 1
    ;;
  -B=* | --base-url=*)
    BASE_URL="${1#*=}"
    shift 1
    ;;
  -v=* | --version=*)
    COOLPACK_VERSION="${1#*=}"
    shift 1
    ;;
  -V=* | --verbose=*)
    VERBOSE="${1#*=}"
    shift 1
    ;;
  -f=* | -y=* | --force=* | --yes=*)
    FORCE="${1#*=}"
    shift 1
    ;;
  *)
    error "Unknown option: $1"
    exit 1
    ;;
  esac
done

# non-empty VERBOSE enables verbose output
if [ -n "${VERBOSE-}" ]; then
  VERBOSE=1
else
  VERBOSE=
fi

if [ $UNINSTALL == 1 ]; then
  confirm "Are you sure you want to uninstall coolpack?"

  msg=""
  sudo=""

  info "Removing coolpack"

  if test_writeable "$(dirname "$(which coolpack)")"; then
    sudo=""
    msg="Removing coolpack, please wait…"
  else
    warn "Escalated permissions are required to remove from ${BIN_DIR}"
    elevate_priv
    sudo="sudo"
    msg="Removing coolpack as root, please wait…"
  fi

  info "$msg"
  ${sudo} rm "$(which coolpack)"

  completed "Removed coolpack"
  exit 0
fi

if [ $HELP == 1 ]; then
  echo "${help_text}"
  exit 0
fi

TARGET="$(detect_target "${ARCH}" "${PLATFORM}")"

is_build_available "${ARCH}" "${PLATFORM}" "${TARGET}"

# Get latest version if not specified
if [ -z "${COOLPACK_VERSION}" ]; then
  info "Fetching latest version..."
  COOLPACK_VERSION="$(get_latest_version)"
fi

print_configuration() {
  if [[ -n "${VERBOSE-}" ]]; then
    printf "  %s\n" "${UNDERLINE}Configuration${NO_COLOR}"
    debug "${BOLD}Bin directory${NO_COLOR}: ${GREEN}${BIN_DIR}${NO_COLOR}"
    debug "${BOLD}Platform${NO_COLOR}:      ${GREEN}${PLATFORM}${NO_COLOR}"
    debug "${BOLD}Arch${NO_COLOR}:          ${GREEN}${ARCH}${NO_COLOR}"
    debug "${BOLD}Version${NO_COLOR}:       ${GREEN}${COOLPACK_VERSION}${NO_COLOR}"
    printf '\n'
  fi
}

print_configuration

EXT=tar.gz

URL="${BASE_URL}/download/${COOLPACK_VERSION}/coolpack_${COOLPACK_VERSION}_${PLATFORM}_${ARCH}.${EXT}"
debug "Tarball URL: ${UNDERLINE}${BLUE}${URL}${NO_COLOR}"
confirm "Install coolpack ${GREEN}${COOLPACK_VERSION}${NO_COLOR} to ${BOLD}${GREEN}${BIN_DIR}${NO_COLOR}?"
check_bin_dir "${BIN_DIR}"

install "${EXT}"

printf "$GREEN"
cat <<'EOF'

   ██████╗ ██████╗  ██████╗ ██╗     ██████╗  █████╗  ██████╗██╗  ██╗
  ██╔════╝██╔═══██╗██╔═══██╗██║     ██╔══██╗██╔══██╗██╔════╝██║ ██╔╝
  ██║     ██║   ██║██║   ██║██║     ██████╔╝███████║██║     █████╔╝
  ██║     ██║   ██║██║   ██║██║     ██╔═══╝ ██╔══██║██║     ██╔═██╗
  ╚██████╗╚██████╔╝╚██████╔╝███████╗██║     ██║  ██║╚██████╗██║  ██╗
   ╚═════╝ ╚═════╝  ╚═════╝ ╚══════╝╚═╝     ╚═╝  ╚═╝ ╚═════╝╚═╝  ╚═╝

EOF
printf "$NO_COLOR"

completed "Installed coolpack ${COOLPACK_VERSION} to ${BIN_DIR}"
info "Run ${BOLD}coolpack --help${NO_COLOR} to get started"
