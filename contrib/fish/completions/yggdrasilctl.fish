complete -c yggdrasilctl -f
complete -c yggdrasilctl -o borders -d 'Output borders on tables'
complete -c yggdrasilctl -o endpoint -r -d 'Admin socket endpoint'
complete -c yggdrasilctl -o json -d 'Output in JSON format'
complete -c yggdrasilctl -n __yggdrasilctl_cmds
complete -c yggdrasilctl -n '__fish_seen_subcommand_from removepeer' -r -a '(__fish_complete_yggdrasilctl_peers)'

function __yggdrasilctl_endpoint
    set -l tokens (commandline -xpc)
    set -e tokens[1]
    argparse -us 'endpoint=' -- $tokens
    or return 0
    set -q _flag_endpoint
    and echo -- -endpoint=$_flag_endpoint
end

function __yggdrasilctl_cmds
    __fish_use_subcommand
    and command -q jq
    and yggdrasilctl (__yggdrasilctl_endpoint) --json list \
        | jq --raw-output0 '.list | map("\(.command) \(.description)") | .[]' \
        | while read -z cmd desc
        complete -c yggdrasilctl -a $cmd -d $desc -n __fish_use_subcommand
    end
end

function __fish_complete_yggdrasilctl_peers
    yggdrasilctl (__yggdrasilctl_endpoint) -borders=0 getpeers | while read -t uri etc
        test "$uri" != URI
        and echo uri=$uri
    end
end
