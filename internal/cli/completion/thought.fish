# fish completion for thought. Install with:
#   thought completion fish > ~/.config/fish/completions/thought.fish

# __thought_candidates prints the candidates for the token under the cursor.
function __thought_candidates
    set -l tokens (commandline -opc)
    set -l cur (commandline -ct)
    set -e tokens[1]
    if test (count $tokens) -eq 0
        thought complete verbs -- $cur
        return
    end
    set -l verb $tokens[1]
    set -e tokens[1]

    switch "$tokens[-1]"
        case -t
            thought complete tags -- $cur
            return
        case -s
            __thought_keyvalue $cur
            return
        case --sort
            printf '%s\n' updated created title rank
            return
        case --body-file --old-file --new-file
            __fish_complete_path $cur
            return
        case --limit --offset --version
            return
    end

    # pos is the position of the token under the cursor among the verb's
    # arguments, flags and their values left out.
    set -l pos 1
    set -l skip 0
    for t in $tokens
        if test $skip -eq 1
            set skip 0
        else if contains -- $t -t -s --body-file --old-file --new-file --sort --limit --offset --version
            set skip 1
        else if not string match -q -- '-?*' $t
            set pos (math $pos + 1)
        end
    end

    switch $verb
        case help
            test $pos -eq 1; and thought complete verbs -- $cur
        case completion
            test $pos -eq 1; and printf '%s\n' fish zsh bash
        case attrs
            test $pos -eq 1; and thought complete keys -- $cur
        case show rm
            thought complete ids -- $cur
        case body write edit replace title
            test $pos -eq 1; and thought complete ids -- $cur
        case tag untag
            if test $pos -eq 1
                thought complete ids -- $cur
            else
                thought complete tags -- $cur
            end
        case set
            if test $pos -eq 1
                thought complete ids -- $cur
            else
                __thought_keyvalue $cur
            end
        case unset
            if test $pos -eq 1
                thought complete ids -- $cur
            else
                thought complete keys -- $cur
            end
        case status
            switch $pos
                case 1
                    thought complete ids -- $cur
                case 2
                    thought complete values status -- $cur
            end
    end
end

# __thought_keyvalue completes key=, then the key's values.
function __thought_keyvalue -a cur
    if string match -q -- '*=*' $cur
        set -l kv (string split -m 1 = -- $cur)
        thought complete values $kv[1] -- $kv[2] | string replace -r -- '^' "$kv[1]="
    else
        thought complete keys -- $cur | string replace -r '$' =
    end
end

complete -c thought -f -a '(__thought_candidates)'
