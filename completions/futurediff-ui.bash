_futurediff_ui_complete() {
  local cur="${COMP_WORDS[COMP_CWORD]}"
  COMPREPLY=( $(compgen -W "doctor status config completion help" -- "$cur") )
}
complete -F _futurediff_ui_complete futurediff-ui
