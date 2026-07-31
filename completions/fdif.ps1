Register-ArgumentCompleter -Native -CommandName fdif -ScriptBlock {
  param($wordToComplete, $commandAst, $cursorPosition)
  $commands = @('start','new','create','status','workspace','shell','review','seal','verify','approve','publish','apply','commit','finish','transactions','list','use','events','abort','discard','daemon','doctor','config','demo','completion','version','help')
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
    'finish' { @('--github','--remote','--base','--title','--body','--body-file','--github-credential','--full','--yes') }
    default { @() }
  }
  $values | Where-Object { $_ -like "$wordToComplete*" } | ForEach-Object {
    [System.Management.Automation.CompletionResult]::new($_, $_, 'ParameterValue', $_)
  }
}
