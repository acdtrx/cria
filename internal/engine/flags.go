package engine

import "cria/internal/config"

// bareFlag is the value that spells a flag taking no value at all, the way the
// servers' own config format spells it.
const bareFlag = "true"

// Flags spells a launch's merged args as the command line a server takes: the
// key becomes the flag, and the value follows it as its own argument — never
// glued on with an '=', so a value holding spaces survives.
//
// A key is the server's own long option written without its dashes, so one
// letter takes one dash and anything longer takes two. A short alias of more
// than one letter (llama's -ngl) therefore has no spelling here: it reaches the
// server as --ngl and is refused there, by name (docs/specs/CONFIG.md).
//
// Composing the same merged args as a section of the servers' own config format
// is the other spelling of this one merge, and it needs no flag rule at all —
// each arg is already one line of it (config.Arg.String).
func Flags(args []config.Arg) []string {
	flags := make([]string, 0, 2*len(args))
	for _, arg := range args {
		flags = append(flags, flagName(arg.Key))
		if arg.Value == bareFlag {
			continue
		}
		flags = append(flags, arg.Value)
	}
	return flags
}

// flagName dashes a key: one dash for a one-letter option, two for the rest.
func flagName(key string) string {
	if len([]rune(key)) == 1 {
		return "-" + key
	}
	return "--" + key
}
