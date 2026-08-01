_fdif_complete() {
  local cur="${COMP_WORDS[COMP_CWORD]}"
  local commands="start new create status workspace shell review seal verify approve publish apply commit finish transactions list use events abort discard daemon doctor config menu demo completion version help"
  if [[ $COMP_CWORD -eq 1 ]]; then
    COMPREPLY=( $(compgen -W "$commands" -- "$cur") )
    return
  fi
  case "${COMP_WORDS[1]}" in
    daemon) COMPREPLY=( $(compgen -W "status start stop restart logs" -- "$cur") ) ;;
    completion) COMPREPLY=( $(compgen -W "bash zsh fish powershell" -- "$cur") ) ;;
    config) COMPREPLY=( $(compgen -W "--explain" -- "$cur") ) ;;
    finish) COMPREPLY=( $(compgen -W "--github --remote --base --title --body --body-file --credential-config --github-credential --full --yes" -- "$cur") ) ;;
  esac
}
complete -F _fdif_complete fdif
