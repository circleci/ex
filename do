#!/usr/bin/env bash
set -eu -o pipefail

reportDir="test-reports"

gitref=$(git rev-parse --short HEAD 2>/dev/null || echo latest)
if [[ "${CI-}" == "true" ]]; then
    if [[ "${CIRCLE_BRANCH-}" == "main" ]]; then
        readonly _version="v1.0.${CIRCLE_BUILD_NUM-0}-${gitref}"
    else
        readonly _version="v0.1.${CIRCLE_BUILD_NUM-0}-branch-${gitref}"
    fi
else
    readonly _version="v0.0.0-local-${gitref}"
fi

help_check_gomod="Check go.mod is tidy"
check-gomod() {
    go mod tidy -v
    git diff --exit-code -- go.mod go.sum
}

help_check_rootcerts="Check rootcerts is up to date"
check-rootcerts() {
    generate
    git diff --ignore-matching-lines='Generated on ' --exit-code -- ./rootcerts
}

# This variable is used, but shellcheck can't tell.
# shellcheck disable=SC2034
help_generate="generate any generated code"
generate() {
    go generate -x ./...
    run-goimports ./rootcerts
}


# This variable is used, but shellcheck can't tell.
# shellcheck disable=SC2034
help_run_goimports="Run goimports for package"
run-goimports () {
  command -v ./bin/gosimports > /dev/null || install-go-bin "github.com/rinchsan/gosimports/cmd/gosimports@v0.1.5"

  local files
  files=$(find . \( -name '*.go' -not -path "./example/*" \))
  ./bin/gosimports -local "github.com/circleci/ex" -w $files
}

# This variable is used, but shellcheck can't tell.
# shellcheck disable=SC2034
help_lint="Run golanci-lint to lint go files."
lint() {
    ./bin/golangci-lint run "${@:-./...}"

    local files
    files=$(find . \( -name '*.go' -not -path "./example*" \))
    if [ -n "$(./bin/gosimports -local "github.com/circleci/ex" -d $files)" ]; then
        echo "Go imports check failed, please run ./do run-goimports"
        exit 1
    fi
}

help_lint_report="Run golanci-lint to lint go files and generate an xml report."
lint-report() {
    output="${reportDir}/lint.xml"
    echo "Storing results as Junit XML in '${output}'" >&2
    mkdir -p "${reportDir}"

    lint --out-format junit-xml | tee "${output}"
}

help_test="Run the tests"
test() {
    mkdir -p "${reportDir}"
    # -count=1 is used to forcibly disable test result caching
    ./bin/gotestsum --junitfile="${reportDir}/junit.xml" -- -count=1 "${@:-./...}"
}

help_release="Make a release"
release() {
    echo "Checking for code changes:"
    git diff --exit-code

    echo "Tagging version: '${_version}'"
    git tag -a "${_version}" -m "Releasing version ${_version}"

    echo "Pushing tag: '${_version}'"
    git push origin "${_version}"
}

help_go_mod_tidy="Run 'go mod tidy' to clean up module files."
go-mod-tidy() {
    go mod tidy -v
}

# Attempt to download go binary tools from github correctly
# go binary releases are somewhat consistent thanks to goreleaser
# however they're not actually that consistent, so this is a pain
# if this is causing more problems than it solves, throw it away
install-github-binary() {
    local org=$1 # github org
    local repo=$2 # github repo == binary name
    local vs=$3 # version separator in tarball filename
    local winext=$4 # archive extension on windows
    local version=$5 # desired version number

    if ./bin/$repo --version | grep "$version" > /dev/null; then
        return
    fi

    local os=$(go env GOHOSTOS)
    local arch=$(go env GOARCH)

    local ext='.tar.gz'
    if [[ "$os" = "windows" ]]; then
        local ext="$winext"
    fi

    local unpack='tar xvzf'
    if [[ "$ext" = ".zip" ]]; then
        local unpack='unzip'
    fi

    local tmp=$(mktemp -d ${TMPDIR:-/tmp/}do-install-github-binary.XXXXXX)
    trap "{ rm -rf $tmp; }" EXIT

    set -x
    mkdir -p ./bin

    curl --fail --location --output "$tmp/download" \
        "https://github.com/$org/$repo/releases/download/v${version}/${repo}${vs}${version}${vs}${os}${vs}${arch}${ext}"

    pushd "$tmp"
    $unpack "$tmp/download"
    popd

    local binary=$(find "$tmp" -name "$repo*" -type f)
    chmod +x "$binary"
    mv "$binary" ./bin/
}

install-go-bin() {
    for pkg in "${@}"; do
        GOBIN="${PWD}/bin" go install "${pkg}" &
    done
    wait
}

help_install_devtools="Install tools that other tasks expect into ./bin"
install-devtools() {
    install-github-binary golangci golangci-lint '-' '.zip' 1.45.0
    install-github-binary gotestyourself gotestsum '_' '.tar.gz' 1.7.0

    install-go-bin \
            "github.com/gwatts/rootcerts/gencerts@v0.0.0-20210602134037-977e162fa4a7" \
            "github.com/rinchsan/gosimports/cmd/gosimports@v0.1.5"
}

help_create_stub_test_files="Create an empty pkg_test in all directories with no tests.

Creating this blank test file will ensure that coverage considers all
packages, not just those with tests.
"
create-stub-test-files() {
    # Constraints:
    # - `go list` (using go templates) cannot use a zero byte as a separator.
    # - `xargs` interprets `\` incorrectly on Windows.
    # Solution
    # - Use `\n` as a string separator in `go list`.
    # - Use `tr` to replace `\n` with zero byte.
    # - Use zero-byte separator switch in `xargs`
    go list -f $'{{if not .TestGoFiles}}{{.Name}}\n{{.Dir}}\n{{end}}' ./... \
        | tr '\n' '\000' \
        | xargs -0 -n 2 \
        sh -c '[ "$0" = "sh" ] && exit 0; echo "package $0" > "$1/pkg_test.go"'
}

help_version="Print version"
version() {
    echo "$_version"
}

help-text-intro() {
   echo "
DO

A set of simple repetitive tasks that adds minimally
to standard tools used to build and test.
(e.g. go and docker)
"
}

### START FRAMEWORK ###
# Do Version 0.0.4
# This variable is used, but shellcheck can't tell.
# shellcheck disable=SC2034
help_self_update="Update the framework from a file.

Usage: $0 self-update FILENAME
"
self-update() {
    local source selfpath pattern
    source="$1"
    selfpath="${BASH_SOURCE[0]}"
    cp "$selfpath" "$selfpath.bak"
    pattern='/### START FRAMEWORK/,/END FRAMEWORK ###$/'
    (sed "${pattern}d" "$selfpath"; sed -n "${pattern}p" "$source") \
        > "$selfpath.new"
    mv "$selfpath.new" "$selfpath"
    chmod --reference="$selfpath.bak" "$selfpath"
}

# This variable is used, but shellcheck can't tell.
# shellcheck disable=SC2034
help_completion="Print shell completion function for this script.

Usage: $0 completion SHELL"
completion() {
    local shell
    shell="${1-}"

    if [ -z "$shell" ]; then
      echo "Usage: $0 completion SHELL" 1>&2
      exit 1
    fi

    case "$shell" in
      bash)
        (echo
        echo '_dotslashdo_completions() { '
        # shellcheck disable=SC2016
        echo '  COMPREPLY=($(compgen -W "$('"$0"' list)" "${COMP_WORDS[1]}"))'
        echo '}'
        echo 'complete -F _dotslashdo_completions '"$0"
        );;
      zsh)
cat <<EOF
_dotslashdo_completions() {
  local -a subcmds
  subcmds=()
  DO_HELP_SKIP_INTRO=1 $0 help | while read line; do
EOF
cat <<'EOF'
    cmd=$(cut -f1  <<< $line)
    cmd=$(awk '{$1=$1};1' <<< $cmd)

    desc=$(cut -f2- <<< $line)
    desc=$(awk '{$1=$1};1' <<< $desc)

    subcmds+=("$cmd:$desc")
  done
  _describe 'do' subcmds
}

compdef _dotslashdo_completions do
EOF
        ;;
     fish)
cat <<EOF
complete -e -c do
complete -f -c do
for line in (string split \n (DO_HELP_SKIP_INTRO=1 $0 help))
EOF
cat <<'EOF'
  set cmd (string split \t $line)
  complete -c do  -a $cmd[1] -d $cmd[2]
end
EOF
    ;;
    esac
}

list() {
    declare -F | awk '{print $3}'
}

# This variable is used, but shellcheck can't tell.
# shellcheck disable=SC2034
help_help="Print help text, or detailed help for a task."
help() {
    local item
    item="${1-}"
    if [ -n "${item}" ]; then
      local help_name
      help_name="help_${item//-/_}"
      echo "${!help_name-}"
      return
    fi

    if [ -z "${DO_HELP_SKIP_INTRO-}" ]; then
      type -t help-text-intro > /dev/null && help-text-intro
    fi
    for item in $(list); do
      local help_name text
      help_name="help_${item//-/_}"
      text="${!help_name-}"
      [ -n "$text" ] && printf "%-30s\t%s\n" "$item" "$(echo "$text" | head -1)"
    done
}

case "${1-}" in
  list) list;;
  ""|"help") help "${2-}";;
  *)
    if ! declare -F "${1}" > /dev/null; then
        printf "Unknown target: %s\n\n" "${1}"
        help
        exit 1
    else
        "$@"
    fi
  ;;
esac
### END FRAMEWORK ###
