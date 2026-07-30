package guidedcli

import (
	"errors"
	"fmt"
	"strings"
)

var fdifCommands = []string{
	"start", "new", "create", "status", "workspace", "shell", "review", "seal",
	"verify", "approve", "publish", "apply", "commit", "finish",
	"transactions", "list", "use", "events", "abort", "discard", "daemon", "doctor",
	"config", "demo", "completion", "version", "help",
}

func completionScript(shell string) (string, error) {
	commands := strings.Join(fdifCommands, " ")
	switch strings.ToLower(shell) {
	case "bash":
		return fmt.Sprintf(`_fdif_complete() {
  local cur="${COMP_WORDS[COMP_CWORD]}"
  local commands=%q
  if [[ $COMP_CWORD -eq 1 ]]; then
    COMPREPLY=( $(compgen -W "$commands" -- "$cur") )
    return
  fi
  case "${COMP_WORDS[1]}" in
    daemon) COMPREPLY=( $(compgen -W "status start stop restart logs" -- "$cur") ) ;;
    completion) COMPREPLY=( $(compgen -W "bash zsh fish powershell" -- "$cur") ) ;;
  esac
}
complete -F _fdif_complete fdif
`, commands), nil
	case "zsh":
		return fmt.Sprintf(`#compdef fdif
_fdif() {
  local -a commands
  commands=(%s)
  if (( CURRENT == 2 )); then
    _describe 'command' commands
    return
  fi
  case "$words[2]" in
    daemon) _values 'action' status start stop restart logs ;;
    completion) _values 'shell' bash zsh fish powershell ;;
  esac
}
compdef _fdif fdif
`, strings.Join(fdifCommands, " ")), nil
	case "fish":
		var b strings.Builder
		b.WriteString("complete -c fdif -f\n")
		for _, command := range fdifCommands {
			fmt.Fprintf(&b, "complete -c fdif -n '__fish_use_subcommand' -a %s\n", command)
		}
		b.WriteString("complete -c fdif -n '__fish_seen_subcommand_from daemon' -a 'status start stop restart logs'\n")
		b.WriteString("complete -c fdif -n '__fish_seen_subcommand_from completion' -a 'bash zsh fish powershell'\n")
		return b.String(), nil
	case "powershell", "pwsh":
		return fmt.Sprintf(`Register-ArgumentCompleter -Native -CommandName fdif -ScriptBlock {
  param($wordToComplete, $commandAst, $cursorPosition)
  $commands = @(%s)
  $tokens = $commandAst.CommandElements
  if ($tokens.Count -le 2) {
    $commands | Where-Object { $_ -like "$wordToComplete*" } | ForEach-Object {
      [System.Management.Automation.CompletionResult]::new($_, $_, 'ParameterValue', $_)
    }
    return
  }
  $parent = $tokens[1].Value
  $values = switch ($parent) {
    'daemon' { @('status','start','stop','restart','logs') }
    'completion' { @('bash','zsh','fish','powershell') }
    default { @() }
  }
  $values | Where-Object { $_ -like "$wordToComplete*" } | ForEach-Object {
    [System.Management.Automation.CompletionResult]::new($_, $_, 'ParameterValue', $_)
  }
}
`, quotePowerShellArray(fdifCommands)), nil
	default:
		return "", errors.New("completion shell must be bash, zsh, fish, or powershell")
	}
}

func quotePowerShellArray(values []string) string {
	quoted := make([]string, 0, len(values))
	for _, value := range values {
		quoted = append(quoted, "'"+strings.ReplaceAll(value, "'", "''")+"'")
	}
	return strings.Join(quoted, ",")
}
