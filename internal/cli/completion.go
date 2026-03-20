package cli

import (
	"errors"
	"strings"
)

func runCompletion(ctx *Context, args []string) (any, int, error) {
	if len(args) == 0 {
		return nil, 0, &usageError{Text: usageCompletion(), ExitCode: 1}
	}
	if args[0] == "-h" || args[0] == "--help" {
		return nil, 0, &usageError{Text: usageCompletion(), ExitCode: 0}
	}
	shell := strings.ToLower(strings.TrimSpace(args[0]))
	switch shell {
	case "bash":
		return completionBash(), 0, nil
	case "zsh":
		return completionZsh(), 0, nil
	case "fish":
		return completionFish(), 0, nil
	case "powershell":
		return completionPowerShell(), 0, nil
	default:
		return nil, 0, errors.New("unsupported shell: " + args[0])
	}
}

func completionGroups() []string {
	return []string{
		"configure",
		"api",
		"project",
		"topic",
		"metric-topic",
		"index",
		"log",
		"assistant",
		"doctor",
		"completion",
	}
}

func completionGlobalFlags() []string {
	return []string{
		"--profile",
		"--output",
		"--output-mode",
		"--output-file",
		"--jmes-filter",
		"--trace-dir",
		"--trace-redact",
		"--secrets-file",
		"--debug",
		"--help",
		"-h",
		"--version",
	}
}

func completionBash() string {
	groups := strings.Join(completionGroups(), " ")
	flags := strings.Join(completionGlobalFlags(), " ")
	return `# bash completion for volclog
_volclog_complete() {
  local cur prev
  COMPREPLY=()
  cur="${COMP_WORDS[COMP_CWORD]}"
  prev="${COMP_WORDS[COMP_CWORD-1]}"

  local i group cmd
  i=1
  while [[ $i -lt ${#COMP_WORDS[@]} ]]; do
    local w="${COMP_WORDS[$i]}"
    if [[ "$w" != -* ]]; then
      group="$w"
      cmd="${COMP_WORDS[$((i+1))]}"
      break
    fi
    case "$w" in
      --profile|--output|--output-mode|--output-file|--jmes-filter|--trace-dir|--trace-redact|--secrets-file)
        i=$((i+2))
        ;;
      --debug|--help|-h|--version)
        i=$((i+1))
        ;;
      *)
        i=$((i+1))
        ;;
    esac
  done

  if [[ "${cur}" == -* ]]; then
    if [[ "$group" == "api" && "$cmd" == "call" ]]; then
      COMPREPLY=( $(compgen -W "--method --path --query --header --body ` + flags + `" -- "${cur}") )
      return 0
    fi
    COMPREPLY=( $(compgen -W "` + flags + `" -- "${cur}") )
    return 0
  fi

  if [[ -z "$group" || ${COMP_CWORD} -eq $i ]]; then
    COMPREPLY=( $(compgen -W "` + groups + `" -- "${cur}") )
    return 0
  fi

  if [[ ${COMP_CWORD} -eq $((i+1)) ]]; then
    case "$group" in
      api)
        COMPREPLY=( $(compgen -W "call" -- "${cur}") )
        return 0
        ;;
      assistant)
        COMPREPLY=( $(compgen -W "describe-session-answer" -- "${cur}") )
        return 0
        ;;
      completion)
        COMPREPLY=( $(compgen -W "bash zsh fish powershell" -- "${cur}") )
        return 0
        ;;
    esac
  fi

  if [[ "$group" == "api" && "$cmd" == "call" ]]; then
    if [[ "$prev" == "--method" ]]; then
      COMPREPLY=( $(compgen -W "GET POST PUT DELETE" -- "${cur}") )
      return 0
    fi
    if [[ "$prev" == "--path" ]]; then
      COMPREPLY=( $(compgen -W "/DescribeProjects /DescribeProject /CreateProject /ModifyProject /DeleteProject /DescribeTopics /DescribeTopic /CreateTopic /ModifyTopic /DeleteTopic /DescribeIndex /CreateIndex /ModifyIndex /SearchLogs" -- "${cur}") )
      return 0
    fi
  fi

  return 0
}
complete -F _volclog_complete volclog
`
}

func completionZsh() string {
	groups := strings.Join(completionGroups(), " ")
	return `#compdef volclog
_volclog() {
  local -a groups global_flags
  local -a configure_cmds api_cmds project_cmds topic_cmds metric_topic_cmds index_cmds log_cmds assistant_cmds completion_args prom_cmds
  local -a api_call_flags http_methods common_api_paths
  local -a configure_set_flags
  local group cmd i w

  groups=(` + groups + `)
  global_flags=(--profile --output --output-mode --output-file --jmes-filter --trace-dir --trace-redact --secrets-file --debug --help -h --version)

  configure_cmds=(set use show list delete)
  api_cmds=(call)
  project_cmds=(list get create modify delete)
  topic_cmds=(list get create modify delete)
  metric_topic_cmds=(list get create modify delete search prom)
  prom_cmds=(query query-range series labels label-values)
  index_cmds=(get create modify)
  log_cmds=(search export export-analysis)
  assistant_cmds=(describe-session-answer)
  completion_args=(bash zsh fish powershell)
  api_call_flags=(--method --path --query --header --body)
  http_methods=(GET POST PUT DELETE)
  common_api_paths=(/DescribeProjects /DescribeProject /CreateProject /ModifyProject /DeleteProject /DescribeTopics /DescribeTopic /CreateTopic /ModifyTopic /DeleteTopic /DescribeIndex /CreateIndex /ModifyIndex /SearchLogs)
  configure_set_flags=(--profile --cred-ref --ak --sk --token --region --endpoint --timeout-seconds)

  group=""
  cmd=""
  i=2
  while (( i <= $#words )); do
    w="$words[$i]"
    case "$w" in
      (--profile|--output|--output-mode|--output-file|--jmes-filter|--trace-dir|--trace-redact|--secrets-file)
        (( i += 2 ))
        ;;
      (--debug|--help|-h|--version)
        (( i += 1 ))
        ;;
      (-*)
        (( i += 1 ))
        ;;
      (*)
        group="$w"
        cmd="$words[$((i+1))]"
        break
        ;;
    esac
  done

  if [[ "$words[$CURRENT]" == -* ]]; then
    if [[ "$group" == "api" && "$cmd" == "call" ]]; then
      local -a flags
      flags=($api_call_flags $global_flags)
      _describe 'flag' flags
    elif [[ "$group" == "configure" && "$cmd" == "set" ]]; then
      local -a flags
      flags=($configure_set_flags $global_flags)
      _describe 'flag' flags
    else
      _describe 'flag' global_flags
    fi
    return 0
  fi

  local -a argspec
  argspec=(
    '(-h --help)'{-h,--help}'[show help]'
    '--profile[profile name]:profile:'
    '--output[output format]:format:(json jsonl)'
    '--output-mode[output destination]:mode:(stdout file)'
    '--output-file[output file path]:file:_files'
    '--jmes-filter[output filter expr]:expr:'
    '--trace-dir[trace artifact dir]:dir:_files -/'
    '--trace-redact[trace redact mode]:mode:(strict default)'
    '--secrets-file[dotenv file]:file:_files'
    '--debug[enable debug]'
    '--version[show version]'
    '1:group:->group'
    '2:command:->cmd'
    '*::args:->args'
  )
  if [[ "$group" == "api" && "$cmd" == "call" ]]; then
    argspec=(
      '(-h --help)'{-h,--help}'[show help]'
      '--profile[profile name]:profile:'
      '--output[output format]:format:(json jsonl)'
      '--output-mode[output destination]:mode:(stdout file)'
      '--output-file[output file path]:file:_files'
      '--jmes-filter[output filter expr]:expr:'
      '--trace-dir[trace artifact dir]:dir:_files -/'
      '--trace-redact[trace redact mode]:mode:(strict default)'
      '--secrets-file[dotenv file]:file:_files'
      '--debug[enable debug]'
      '--version[show version]'
      '--method[HTTP method]:method:(GET POST PUT DELETE)'
      '--path[API path (starts with "/")]:path:->apipath'
      '--query[query k=v (repeatable)]:query:'
      '--header[header k=v (repeatable)]:header:'
      '--body[body (json|file://...|-)]:body:_files'
      '1:group:->group'
      '2:command:->cmd'
      '*::args:->args'
    )
  fi
  if [[ "$group" == "configure" && "$cmd" == "set" ]]; then
    argspec=(
      '(-h --help)'{-h,--help}'[show help]'
      '--profile[profile name]:profile:'
      '--output[output format]:format:(json jsonl)'
      '--output-mode[output destination]:mode:(stdout file)'
      '--output-file[output file path]:file:_files'
      '--jmes-filter[output filter expr]:expr:'
      '--trace-dir[trace artifact dir]:dir:_files -/'
      '--trace-redact[trace redact mode]:mode:(strict default)'
      '--secrets-file[dotenv file]:file:_files'
      '--debug[enable debug]'
      '--version[show version]'
      '--cred-ref[credential name (reuse AK/SK)]:name:'
      '--ak[access key id]:ak:'
      '--sk[secret access key]:sk:'
      '--token[security token (optional)]:token:'
      '--region[region (optional; derived from standard endpoint)]:region:'
      '--endpoint[service endpoint (required)]:endpoint:'
      '--timeout-seconds[http timeout seconds]:seconds:'
      '1:group:->group'
      '2:command:->cmd'
      '*::args:->args'
    )
  fi
  if [[ "$group" == "metric-topic" && "$cmd" == "prom" ]]; then
    argspec=(
      '(-h --help)'{-h,--help}'[show help]'
      '--profile[profile name]:profile:'
      '--output[output format]:format:(json jsonl)'
      '--output-mode[output destination]:mode:(stdout file)'
      '--output-file[output file path]:file:_files'
      '--jmes-filter[output filter expr]:expr:'
      '--trace-dir[trace artifact dir]:dir:_files -/'
      '--trace-redact[trace redact mode]:mode:(strict default)'
      '--secrets-file[dotenv file]:file:_files'
      '--debug[enable debug]'
      '--version[show version]'
      '1:group:->group'
      '2:command:->cmd'
      '3:subcommand:->subcmd'
      '*::args:->args'
    )
  fi
  _arguments -C $argspec

  case $state in
    (group)
      _describe 'group' groups
      ;;
    (cmd)
      case $group in
        (configure) _describe 'command' configure_cmds ;;
        (api) _describe 'command' api_cmds ;;
        (project) _describe 'command' project_cmds ;;
        (topic) _describe 'command' topic_cmds ;;
        (metric-topic) _describe 'command' metric_topic_cmds ;;
        (index) _describe 'command' index_cmds ;;
        (log) _describe 'command' log_cmds ;;
        (assistant) _describe 'command' assistant_cmds ;;
        (completion) _describe 'shell' completion_args ;;
      esac
      ;;
    (apipath)
      _describe 'path' common_api_paths
      ;;
    (subcmd)
      if [[ "$group" == "metric-topic" && "$cmd" == "prom" ]]; then
        _describe 'subcommand' prom_cmds
      fi
      ;;
    (args)
      if [[ "$words[2]" == "api" && "$words[3]" == "call" ]]; then
        if [[ "$words[$CURRENT]" == -* ]]; then
          _describe 'flag' api_call_flags
          return 0
        fi
        if [[ "$words[$((CURRENT-1))]" == "--path" ]]; then
          _describe 'path' common_api_paths
          return 0
        fi
        if [[ "$words[$((CURRENT-1))]" == "--body" ]]; then
          _describe 'body' '(- file://...)' && _files
          return 0
        fi
      fi
      ;;
  esac
}
compdef _volclog volclog
`
}

func completionFish() string {
	var b strings.Builder
	b.WriteString("# fish completion for volclog\n")
	b.WriteString("complete -c volclog -f -n '__fish_use_subcommand' -a '" + strings.Join(completionGroups(), " ") + "'\n")
	for _, f := range completionGlobalFlags() {
		if strings.HasPrefix(f, "--") {
			b.WriteString("complete -c volclog -l " + strings.TrimPrefix(f, "--") + "\n")
		}
	}
	b.WriteString("complete -c volclog -f -n '__fish_seen_subcommand_from api; and __fish_use_subcommand' -a 'call'\n")
	b.WriteString("complete -c volclog -l method -n '__fish_seen_subcommand_from api; and __fish_seen_subcommand_from call' -xa 'GET POST PUT DELETE'\n")
	b.WriteString("complete -c volclog -l path -n '__fish_seen_subcommand_from api; and __fish_seen_subcommand_from call' -xa '/DescribeProjects /DescribeProject /CreateProject /ModifyProject /DeleteProject /DescribeTopics /DescribeTopic /CreateTopic /ModifyTopic /DeleteTopic /DescribeIndex /CreateIndex /ModifyIndex /SearchLogs'\n")
	b.WriteString("complete -c volclog -l query -n '__fish_seen_subcommand_from api; and __fish_seen_subcommand_from call'\n")
	b.WriteString("complete -c volclog -l header -n '__fish_seen_subcommand_from api; and __fish_seen_subcommand_from call'\n")
	b.WriteString("complete -c volclog -l body -n '__fish_seen_subcommand_from api; and __fish_seen_subcommand_from call'\n")
	return b.String()
}

func completionPowerShell() string {
	groups := strings.Join(completionGroups(), "', '")
	return `# PowerShell completion for volclog
Register-ArgumentCompleter -Native -CommandName volclog -ScriptBlock {
  param($wordToComplete, $commandAst, $cursorPosition)

  $groups = @('` + groups + `')
  $globalFlagsWithValue = @('--profile','--output','--output-mode','--output-file','--jmes-filter','--trace-dir','--trace-redact','--secrets-file')
  $globalFlagsBare = @('--debug','--help','-h','--version')
  $apiCallFlags = @('--method','--path','--query','--header','--body')
  $httpMethods = @('GET','POST','PUT','DELETE')
  $apiPaths = @('/DescribeProjects','/DescribeProject','/CreateProject','/ModifyProject','/DeleteProject','/DescribeTopics','/DescribeTopic','/CreateTopic','/ModifyTopic','/DeleteTopic','/DescribeIndex','/CreateIndex','/ModifyIndex','/SearchLogs')
  $configureCmds = @('set','use','show','list','delete')
  $projectCmds = @('list','get','create','modify','delete')
  $topicCmds = @('list','get','create','modify','delete')
  $metricTopicCmds = @('list','get','create','modify','delete','search','prom')
  $indexCmds = @('get','create','modify')
  $logCmds = @('search','export','export-analysis')
  $doctorCmds = @('--online')
  $promCmds = @('query','query-range','series','labels','label-values')

  $elems = @()
  try { $elems = $commandAst.CommandElements } catch { $elems = @() }

  $args = @()
  if ($elems.Count -gt 1) {
    for ($i = 1; $i -lt $elems.Count; $i++) {
      $t = $elems[$i].ToString()
      if ($t -ne $null -and $t -ne '') { $args += $t }
    }
  }

  $group = ''
  $cmd = ''
  for ($i = 0; $i -lt $args.Count; ) {
    $a = $args[$i]
    if ($a -notmatch '^-') { $group = $a; if ($i + 1 -lt $args.Count) { $cmd = $args[$i+1] }; break }
    if ($globalFlagsWithValue -contains $a) { $i += 2; continue }
    if ($globalFlagsBare -contains $a) { $i += 1; continue }
    $i += 1
  }

  $prev = ''
  if ($args.Count -ge 1) { $prev = $args[$args.Count - 1] }

  $candidates = @()
  if ($wordToComplete -like '-*') {
    if ($group -eq 'api' -and $cmd -eq 'call') {
      $candidates = $apiCallFlags + $globalFlagsWithValue + $globalFlagsBare
    } elseif ($group -eq 'doctor') {
      $candidates = $doctorCmds + $globalFlagsWithValue + $globalFlagsBare
    } else {
      $candidates = $globalFlagsWithValue + $globalFlagsBare
    }
  } else {
    if ($group -eq '') {
      $candidates = $groups
    } elseif ($cmd -eq '') {
      switch ($group) {
        'api' { $candidates = @('call') }
        'configure' { $candidates = $configureCmds }
        'project' { $candidates = $projectCmds }
        'topic' { $candidates = $topicCmds }
        'metric-topic' { $candidates = $metricTopicCmds }
        'index' { $candidates = $indexCmds }
        'log' { $candidates = $logCmds }
        'assistant' { $candidates = @('describe-session-answer') }
        'doctor' { $candidates = @() }
        'completion' { $candidates = @('bash','zsh','fish','powershell') }
      }
    } else {
      if ($group -eq 'api' -and $cmd -eq 'call') {
        if ($prev -eq '--method') { $candidates = $httpMethods }
        elseif ($prev -eq '--path') { $candidates = $apiPaths }
      }
      if ($group -eq 'metric-topic' -and $cmd -eq 'prom') {
        $candidates = $promCmds
      }
    }
  }

  $candidates | Where-Object { $_ -like "$wordToComplete*" } | Sort-Object -Unique | ForEach-Object {
    [System.Management.Automation.CompletionResult]::new($_, $_, 'ParameterValue', $_)
  }
}
`
}
