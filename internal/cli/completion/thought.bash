# bash completion for thought. Install with:
#   thought completion bash > ~/.local/share/bash-completion/completions/thought
# or add `source <(thought completion bash)` to ~/.bashrc.

# _thought_reply sets COMPREPLY from newline-separated candidates. Bash splits
# words at = and :, so the part of cur up to the last of them is dropped
# from each candidate.
_thought_reply() {
    local cur=$1 cands=$2 head c
    head=${cur%"${cur##*[=:]}"}
    local IFS=$'\n'
    for c in $cands; do
        COMPREPLY+=("${c#"$head"}")
    done
}

_thought_ids() {
    _thought_reply "$1" "$(thought complete ids -- "$1" | cut -f1)"
}

_thought_keyvalue() {
    local cur=$1
    if [[ $cur == *=* ]]; then
        local key=${cur%%=*}
        _thought_reply "$cur" "$(thought complete values "$key" -- "${cur#*=}" | sed "s/^/$key=/")"
    else
        _thought_reply "$cur" "$(thought complete keys -- "$cur" | sed 's/$/=/')"
        compopt -o nospace 2>/dev/null
    fi
}

_thought() {
    COMPREPLY=()
    # Words are split on spaces only, so key=value stays one word.
    local line=${COMP_LINE:0:COMP_POINT} cur
    local -a words
    read -ra words <<<"$line"
    if [[ $line == *' ' ]]; then
        cur=
    else
        cur=${words[${#words[@]}-1]}
        unset 'words[${#words[@]}-1]'
    fi
    if (( ${#words[@]} <= 1 )); then
        _thought_reply "$cur" "$(thought complete verbs -- "$cur")"
        return
    fi
    local verb=${words[1]} prev=${words[${#words[@]}-1]}

    case $prev in
        -t) _thought_reply "$cur" "$(thought complete tags -- "$cur")"; return ;;
        -s) _thought_keyvalue "$cur"; return ;;
        --sort) COMPREPLY=($(compgen -W 'updated created title rank' -- "$cur")); return ;;
        --body-file|--old-file|--new-file) COMPREPLY=($(compgen -f -- "$cur")); return ;;
        --limit|--offset|--version) return ;;
    esac

    # pos is the position of the word under the cursor among the verb's
    # arguments, flags and their values left out.
    local pos=1 skip=0 t i
    for (( i = 2; i < ${#words[@]}; i++ )); do
        t=${words[i]}
        if (( skip )); then
            skip=0
        else
            case $t in
                -t|-s|--body-file|--old-file|--new-file|--sort|--limit|--offset|--version) skip=1 ;;
                -?*) ;;
                *) pos=$((pos + 1)) ;;
            esac
        fi
    done

    case $verb in
        help) (( pos == 1 )) && _thought_reply "$cur" "$(thought complete verbs -- "$cur")" ;;
        completion) (( pos == 1 )) && COMPREPLY=($(compgen -W 'fish zsh bash' -- "$cur")) ;;
        attrs) (( pos == 1 )) && _thought_reply "$cur" "$(thought complete keys -- "$cur")" ;;
        show|rm) _thought_ids "$cur" ;;
        body|write|edit|replace|title) (( pos == 1 )) && _thought_ids "$cur" ;;
        tag|untag)
            if (( pos == 1 )); then _thought_ids "$cur"; else _thought_reply "$cur" "$(thought complete tags -- "$cur")"; fi ;;
        set)
            if (( pos == 1 )); then _thought_ids "$cur"; else _thought_keyvalue "$cur"; fi ;;
        unset)
            if (( pos == 1 )); then _thought_ids "$cur"; else _thought_reply "$cur" "$(thought complete keys -- "$cur")"; fi ;;
        status)
            if (( pos == 1 )); then _thought_ids "$cur"
            elif (( pos == 2 )); then _thought_reply "$cur" "$(thought complete values status -- "$cur")"; fi ;;
    esac
    return 0
}

complete -F _thought thought
