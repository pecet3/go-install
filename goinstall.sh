#!/usr/bin/env bash
set -euo pipefail

# this is vibecoded prototype program 


# -------------------------
# CONFIG (można nadpisać)
# -------------------------
TARGET_OS="${TARGET_OS:-}"
TARGET_ARCH="${TARGET_ARCH:-}"
INSTALL_DIR="${INSTALL_DIR:-/usr/local}"
FORCE_INSTALL="${FORCE_INSTALL:-false}"  # true jeśli ma nadpisać istniejącą wersję
VERSION="${VERSION:-}"                   # jeśli pusta → najnowsza
# -------------------------
# FUNKCJE
# -------------------------

SUDO_CMD=""
if [[ $EUID -ne 0 ]]; then
    if command -v sudo >/dev/null 2>&1; then
        SUDO_CMD="sudo"
        info "Running as regular user, will use sudo when needed"
    else
        error "Not running as root and sudo is not available. Please install sudo or run as root."
    fi
else
    info "Running as root, sudo not needed"
fi


info() { echo -e "[\033[1;34mINFO\033[0m] $*"; }
success() { echo -e "[\033[1;32mOK\033[0m] $*"; }
error() { echo -e "[\033[1;31mERROR\033[0m] $*" >&2; exit 1; }
error_only_print() { echo -e "[\033[1;31mERROR\033[0m] $*"; }

detect_os_arch() {
    if [[ -z "$TARGET_OS" ]]; then
        case "$(uname -s)" in
            Linux*) TARGET_OS=linux ;;
            Darwin*) TARGET_OS=darwin ;;
            CYGWIN*|MINGW*) TARGET_OS=windows ;;
            *) error "Unsupported OS: $(uname -s)" ;;
        esac
    fi
    if [[ -z "$TARGET_ARCH" ]]; then
        case "$(uname -m)" in
            x86_64) TARGET_ARCH=amd64 ;;
            arm64|aarch64) TARGET_ARCH=arm64 ;;
            i386|i686) TARGET_ARCH=386 ;;
            *) error "Unsupported ARCH: $(uname -m)" ;;
        esac
    fi
    info "Detected OS=$TARGET_OS ARCH=$TARGET_ARCH"
}

check_dependencies() {
    REQUIRED=("$@")
    MISSING=()

    # detect mis sing
    info "Required depedencies: ${REQUIRED[*]}"
    for cmd in "${REQUIRED[@]}"; do
        if ! command -v "$cmd" >/dev/null 2>&1; then
            MISSING+=("$cmd")
        fi
    done

    # none missing
    if [[ ${#MISSING[@]} -eq 0 ]]; then
        info "${REQUIRED[*]} already installed."
        return
    fi

    error_only_print "Missing dependencies: ${MISSING[*]}"

    OS="$(uname -s)"
    PKG_MANAGER=""

    # detect package manager
    case "$OS" in
        Linux)
            if command -v apt >/dev/null 2>&1; then
                PKG_MANAGER="apt"
            elif command -v dnf >/dev/null 2>&1; then
                PKG_MANAGER="dnf"
            elif command -v yum >/dev/null 2>&1; then
                PKG_MANAGER="yum"
            elif command -v pacman >/dev/null 2>&1; then
                PKG_MANAGER="pacman"
            elif command -v zypper >/dev/null 2>&1; then
                PKG_MANAGER="zypper"
            fi
        ;;
        Darwin)
            PKG_MANAGER="brew"
        ;;
    esac

    # ask user
    echo -n "Install missing depedencies? [yes/no]: "
    read -r answer
    answer="${answer,,}"

    [[ "$answer" != "yes" ]] && error "Cannot continue without deps: ${MISSING[*]}"

    case "$PKG_MANAGER" in
        apt)
            sudo apt update
            sudo apt install -y "${MISSING[@]}"
        ;;
        dnf)
            sudo dnf install -y "${MISSING[@]}"
        ;;
        yum)
            sudo yum install -y "${MISSING[@]}"
        ;;
        pacman)
            sudo pacman -Sy --needed "${MISSING[@]}"
        ;;
        zypper)
            sudo zypper install -y "${MISSING[@]}"
        ;;
        brew)
            if ! command -v brew >/dev/null 2>&1; then
                warn "Homebrew not found!"
                echo -n "Install Homebrew? [yes/no]: "
                read -r ans
                ans="${ans,,}"
                [[ "$ans" != "yes" ]] && error "Homebrew required"
                /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
            fi
            brew install "${MISSING[@]}"
        ;;
        *)
            error "Unknown OS / package manager. Install manually: ${MISSING[*]}"
        ;;
    esac

}

fetch_version_info() {
    # dodaj prefiks "go", jeśli użytkownik nie podał

    if [[ -z "$VERSION" ]]; then
        # brak podanej wersji → najnowsza stabilna
        JSON_URL="https://go.dev/dl/?mode=json"
        json=$(curl -s "$JSON_URL")
        VERSION=$(echo "$json" | jq -r '.[0].version')
        info "No VERSION set. Using latest stable: $VERSION"
    else
        [[ -n "$VERSION" && "$VERSION" != go* ]] && VERSION="go$VERSION"

        JSON_URL="https://go.dev/dl/?mode=json&include=all"
        json=$(curl -s "$JSON_URL")

        # sprawdź, czy podana wersja istnieje
        exists=$(echo "$json" | jq -r --arg ver "$VERSION" 'map(select(.version==$ver)) | length')

        if [[ "$exists" -eq 0 ]]; then
            error_only_print "Version $VERSION not found in Go releases."

            # jeśli podano major.minor np. go1.24 lub 1.24
            if [[ "$VERSION" =~ ^go?[0-9]+\.[0-9]+$ ]]; then
                prefix="$VERSION"
                available_versions=($(echo "$json" | jq -r --arg pre "$prefix" '.[] | select(.version|startswith($pre)) | .version'))
            else
                available_versions=($(echo "$json" | jq -r '.[].version'))
            fi

            # jeśli nie ma dostępnych wersji → wyjdź
            if [ ${#available_versions[@]} -eq 0 ]; then
                error_only_print "No matching versions found."
                exit 1
            fi

            echo "Available versions:"
            for i in "${!available_versions[@]}"; do
                echo "$i) ${available_versions[$i]}"
            done

            # zapytaj o wybór
            read -p "Select a version by number or type the exact version: " choice

            # jeśli wpisano numer → wybierz wersję
            if [[ "$choice" =~ ^[0-9]+$ ]] && [ "$choice" -ge 0 ] && [ "$choice" -lt ${#available_versions[@]} ]; then
                VERSION="${available_versions[$choice]}"
            else
                VERSION="$choice"
            fi

            info "changed to VERSION=$VERSION"
            run_install
            cleanup
            fi

            info "Using user-specified version: $VERSION"
    fi
}

select_file_and_checksum() {
    FILE_NAME=$(echo "$json" | jq -r --arg ver "$VERSION" --arg os "$TARGET_OS" --arg arch "$TARGET_ARCH" '
        .[] | select(.version==$ver) | .files[] | select(.os==$os and .arch==$arch and .kind=="archive") | .filename
    ')

    FILE_SHA256=$(echo "$json" | jq -r --arg ver "$VERSION" --arg file "$FILE_NAME" '
        .[] | select(.version==$ver) | .files[] | select(.filename==$file) | .sha256
    ')

    if [[ -z "$FILE_NAME" || -z "$FILE_SHA256" ]]; then
        error "No matching build found for OS=$TARGET_OS ARCH=$TARGET_ARCH"
    fi

    info "Will download: $FILE_NAME"
    info "SHA256: $FILE_SHA256"
}

download_and_verify() {
    curl -LO "https://go.dev/dl/$FILE_NAME"
    echo "$FILE_SHA256  $FILE_NAME" | sha256sum -c -
    success "SHA256 verified"
}

backup_old_go() {
    if [[ -d "$INSTALL_DIR/go" ]]; then
        sudo rm -rf "$INSTALL_DIR/go"
    else
        error_only_print "Go already installed in $INSTALL_DIR/go."
        echo "Do you want to override? [yes/no]"
        read -r answer
        answer=${answer,,}
        if [[ "$answer" != "yes" ]]; then
                cleanup
                exit 1
        fi
        sudo rm -rf "$INSTALL_DIR/go"
    fi
}

install_go() {
    info "Extracting $FILE_NAME to $INSTALL_DIR"
    sudo tar -C "$INSTALL_DIR" -xzf "$FILE_NAME"
    success "Go installed to $INSTALL_DIR/go"
}

setup_path() {
    GO_BIN="/usr/local/go/bin"
    SHELL_CONFIGS=("$HOME/.bash_profile" "$HOME/.profile" "$HOME/.bashrc" "$HOME/.zshrc")
    
    # if go already visible in PATH
    if command -v go >/dev/null 2>&1; then
        success "Go visible in PATH ($(go version))."
        return
    fi

    # ensure directory exists
    if [[ ! -d "$GO_BIN" ]]; then
        error "Go binary directory not found: $GO_BIN"
        return 1
    fi

    info "Go not found in PATH, fixing..."

    for file in "${SHELL_CONFIGS[@]}"; do
        # skip missing config files
        [[ ! -f "$file" ]] && continue

        # remove old misformatted go PATHs
        sed -i '/\/usr\/local\/go\/bin/d' "$file"

        # append correct entry
        echo 'export PATH="$PATH:/usr/local/go/bin"' >> "$file"
    done

    # if no config file existed – fallback
    if [[ ! -f "$HOME/.profile" ]]; then
        echo 'export PATH="$PATH:/usr/local/go/bin"' >> "$HOME/.profile"
    fi

    success "PATH updated. Restart your shell or run:"
    echo '   source ~/.profile'
    echo

    success "Installed: $(/usr/local/go/bin/go version || true)"
}


cleanup() {
    rm -f "$FILE_NAME"
}


run_install(){
    fetch_version_info
    select_file_and_checksum
    download_and_verify
    backup_old_go
    info "Checking depedencies required to Go..."
    check_dependencies gcc make git
    install_go
    setup_path
    
}

# -------------------------
# MAIN
# -------------------------
detect_os_arch
info "Checking script depedencies..."
check_dependencies curl jq tar sha256sum

run_install

cleanup

