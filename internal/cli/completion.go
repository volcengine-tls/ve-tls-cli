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
	return cliGroupNames()
}

func completionGlobalFlags() []string {
	return cliGlobalFlags()
}

func completionBashCaseList(flags []string) string {
	return strings.Join(flags, "|")
}

func completionPowerShellArray(values []string) string {
	if len(values) == 0 {
		return ""
	}
	quoted := make([]string, 0, len(values))
	for _, value := range values {
		quoted = append(quoted, "'"+value+"'")
	}
	return strings.Join(quoted, ",")
}

func completionZshGlobalArgspec() []string {
	specs := cliGlobalFlagSpecs()
	lines := make([]string, 0, len(specs))
	for _, spec := range specs {
		switch spec.Name {
		case "--help", "-h":
			continue
		case "--profile":
			lines = append(lines, "'--profile[profile name]:profile:'")
		case "--output":
			lines = append(lines, "'--output[output format]:format:(json jsonl table)'")
		case "--output-mode":
			lines = append(lines, "'--output-mode[output destination]:mode:(stdout file)'")
		case "--output-file":
			lines = append(lines, "'--output-file[output file path]:file:_files'")
		case "--jmes-filter":
			lines = append(lines, "'--jmes-filter[output filter expr]:expr:'")
		case "--trace-dir":
			lines = append(lines, "'--trace-dir[trace artifact dir]:dir:_files -/'")
		case "--trace-redact":
			lines = append(lines, "'--trace-redact[trace redact mode]:mode:(strict default)'")
		case "--secrets-file":
			lines = append(lines, "'--secrets-file[dotenv file]:file:_files'")
		default:
			lines = append(lines, "'"+spec.Name+"["+spec.Description+"]'")
		}
	}
	return lines
}

func completionBash() string {
	groups := strings.Join(completionGroups(), " ")
	flags := strings.Join(completionGlobalFlags(), " ")
	flagsWithValue := completionBashCaseList(cliGlobalFlagsWithValue())
	bareFlags := completionBashCaseList(cliGlobalBareFlags())
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
      ` + flagsWithValue + `)
        i=$((i+2))
        ;;
      ` + bareFlags + `)
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
      project)
        COMPREPLY=( $(compgen -W "list get create modify delete" -- "${cur}") )
        return 0
        ;;
      topic)
        COMPREPLY=( $(compgen -W "list get create modify delete" -- "${cur}") )
        return 0
        ;;
      metric-topic)
        COMPREPLY=( $(compgen -W "list get create modify delete search prom" -- "${cur}") )
        return 0
        ;;
      index)
        COMPREPLY=( $(compgen -W "get create modify" -- "${cur}") )
        return 0
        ;;
      log)
        COMPREPLY=( $(compgen -W "search histogram context put export export-analysis" -- "${cur}") )
        return 0
        ;;
      host-group)
        COMPREPLY=( $(compgen -W "list get bind-rules unbind-rules delete-host create modify delete" -- "${cur}") )
        return 0
        ;;
      collector)
        COMPREPLY=( $(compgen -W "list get bind-host-groups unbind-host-groups create modify delete" -- "${cur}") )
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
	globalFlags := strings.Join(completionGlobalFlags(), " ")
	flagsWithValue := completionBashCaseList(cliGlobalFlagsWithValue())
	bareFlags := completionBashCaseList(cliGlobalBareFlags())
	zshGlobalArgspec := strings.Join(completionZshGlobalArgspec(), "\n    ")
	return `#compdef volclog
_volclog() {
  local -a groups global_flags
  local -a configure_cmds skill_cmds api_cmds project_cmds topic_cmds metric_topic_cmds index_cmds log_cmds host_group_cmds collector_cmds completion_args prom_cmds
  local -a api_call_flags http_methods common_api_paths
  local -a configure_set_flags
  local group cmd i w

  groups=(` + groups + `)
  global_flags=(` + globalFlags + `)

  configure_cmds=(set use show list delete)
  skill_cmds=(list install)
  api_cmds=(call)
  project_cmds=(list get create modify delete)
  topic_cmds=(list get create modify delete)
  metric_topic_cmds=(list get create modify delete search prom)
  prom_cmds=(query query-range series labels label-values)
  index_cmds=(get create modify)
  log_cmds=(search histogram context put export export-analysis)
  host_group_cmds=(list get bind-rules unbind-rules delete-host create modify delete)
  collector_cmds=(list get bind-host-groups unbind-host-groups create modify delete)
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
      (` + flagsWithValue + `)
        (( i += 2 ))
        ;;
      (` + bareFlags + `)
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
    ` + zshGlobalArgspec + `
    '1:group:->group'
    '2:command:->cmd'
    '*::args:->args'
  )
  if [[ "$group" == "api" && "$cmd" == "call" ]]; then
    argspec=(
      '(-h --help)'{-h,--help}'[show help]'
      ` + zshGlobalArgspec + `
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
      ` + zshGlobalArgspec + `
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
      ` + zshGlobalArgspec + `
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
        (skill) _describe 'command' skill_cmds ;;
        (api) _describe 'command' api_cmds ;;
        (project) _describe 'command' project_cmds ;;
        (topic) _describe 'command' topic_cmds ;;
        (metric-topic) _describe 'command' metric_topic_cmds ;;
        (index) _describe 'command' index_cmds ;;
        (log) _describe 'command' log_cmds ;;
        (host-group) _describe 'command' host_group_cmds ;;
        (collector) _describe 'command' collector_cmds ;;
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
	b.WriteString("complete -c volclog -f -n '__fish_seen_subcommand_from project; and __fish_use_subcommand' -a 'list get create modify delete'\n")
	b.WriteString("complete -c volclog -f -n '__fish_seen_subcommand_from topic; and __fish_use_subcommand' -a 'list get create modify delete'\n")
	b.WriteString("complete -c volclog -f -n '__fish_seen_subcommand_from metric-topic; and __fish_use_subcommand' -a 'list get create modify delete search prom'\n")
	b.WriteString("complete -c volclog -f -n '__fish_seen_subcommand_from index; and __fish_use_subcommand' -a 'get create modify'\n")
	b.WriteString("complete -c volclog -f -n '__fish_seen_subcommand_from log; and __fish_use_subcommand' -a 'search histogram context put export export-analysis'\n")
	b.WriteString("complete -c volclog -f -n '__fish_seen_subcommand_from host-group; and __fish_use_subcommand' -a 'list get bind-rules unbind-rules delete-host create modify delete'\n")
	b.WriteString("complete -c volclog -f -n '__fish_seen_subcommand_from collector; and __fish_use_subcommand' -a 'list get bind-host-groups unbind-host-groups create modify delete'\n")
	return b.String()
}

func completionPowerShell() string {
	groups := strings.Join(completionGroups(), "', '")
	globalFlagsWithValue := completionPowerShellArray(cliGlobalFlagsWithValue())
	globalFlagsBare := completionPowerShellArray(cliGlobalBareFlags())
	return `# PowerShell completion for volclog
Register-ArgumentCompleter -Native -CommandName volclog -ScriptBlock {
  param($wordToComplete, $commandAst, $cursorPosition)

  $groups = @('` + groups + `')
  $globalFlagsWithValue = @(` + globalFlagsWithValue + `)
  $globalFlagsBare = @(` + globalFlagsBare + `)
  $apiCallFlags = @('--method','--path','--query','--header','--body')
  $httpMethods = @('GET','POST','PUT','DELETE')
  $apiPaths = @('/DescribeProjects','/DescribeProject','/CreateProject','/ModifyProject','/DeleteProject','/DescribeTopics','/DescribeTopic','/CreateTopic','/ModifyTopic','/DeleteTopic','/DescribeIndex','/CreateIndex','/ModifyIndex','/SearchLogs')
  $configureCmds = @('set','use','show','list','delete')
  $skillCmds = @('list','install')
  $projectCmds = @('list','get','create','modify','delete')
  $topicCmds = @('list','get','create','modify','delete')
  $metricTopicCmds = @('list','get','create','modify','delete','search','prom')
  $indexCmds = @('get','create','modify')
  $logCmds = @('search','histogram','context','put','export','export-analysis')
  $hostGroupCmds = @('list','get','bind-rules','unbind-rules','delete-host','create','modify','delete')
  $collectorCmds = @('list','get','bind-host-groups','unbind-host-groups','create','modify','delete')
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
        'skill' { $candidates = $skillCmds }
        'project' { $candidates = $projectCmds }
        'topic' { $candidates = $topicCmds }
        'metric-topic' { $candidates = $metricTopicCmds }
        'index' { $candidates = $indexCmds }
        'log' { $candidates = $logCmds }
        'host-group' { $candidates = $hostGroupCmds }
        'collector' { $candidates = $collectorCmds }
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
