package app

import (
	"strconv"
	"strings"
)

var performanceLaunchFlags = []string{
	"-useperfthreads",
	"-NoAsyncLoadingThread",
	"-UseMultithreadForDS",
}

func applyLaunchArgSummary(settings *settingsPayload) {
	args, err := splitCommandLine(settings.ServerLaunchArgs)
	if err != nil {
		return
	}
	settings.GamePort = launchArgInt(args, "-port")
	settings.LaunchPlayers = launchArgInt(args, "-players")
	settings.PublicLobby = hasLaunchFlag(args, "-publiclobby")
	settings.NoMods = hasLaunchFlag(args, "-NoMods")
	settings.WorkerThreads = launchArgInt(args, "-NumberOfWorkerThreadsServer")
	settings.PerformanceFlags = hasAllLaunchFlags(args, performanceLaunchFlags)
}

func mergeStructuredLaunchSettings(settings settingsPayload) (string, error) {
	args, err := splitCommandLine(settings.ServerLaunchArgs)
	if err != nil {
		return "", err
	}
	args = setLaunchArgInt(args, "-port", settings.GamePort)
	args = setLaunchArgInt(args, "-players", settings.LaunchPlayers)
	args = setLaunchArgInt(args, "-NumberOfWorkerThreadsServer", settings.WorkerThreads)
	args = setLaunchFlag(args, "-publiclobby", settings.PublicLobby)
	args = setLaunchFlag(args, "-NoMods", settings.NoMods)
	for _, flag := range performanceLaunchFlags {
		args = setLaunchFlag(args, flag, settings.PerformanceFlags)
	}
	return joinLaunchArgs(args), nil
}

func launchArgInt(args []string, key string) int {
	value := launchArgValue(args, key)
	if value == "" {
		return 0
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return 0
	}
	return parsed
}

func launchArgValue(args []string, key string) string {
	keyLower := strings.ToLower(key)
	prefix := keyLower + "="
	for i, arg := range args {
		argLower := strings.ToLower(arg)
		if strings.EqualFold(arg, key) {
			if i+1 < len(args) {
				return strings.TrimSpace(args[i+1])
			}
			return ""
		}
		if strings.HasPrefix(argLower, prefix) {
			return strings.TrimSpace(arg[len(key)+1:])
		}
	}
	return ""
}

func setLaunchArgInt(args []string, key string, value int) []string {
	if value <= 0 {
		return setLaunchArgValue(args, key, "")
	}
	return setLaunchArgValue(args, key, strconv.Itoa(value))
}

func setLaunchArgValue(args []string, key string, value string) []string {
	keyLower := strings.ToLower(key)
	prefix := keyLower + "="
	next := make([]string, 0, len(args)+1)
	replaced := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		argLower := strings.ToLower(arg)
		if argLower == keyLower {
			if value != "" && !replaced {
				next = append(next, key+"="+value)
				replaced = true
			}
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				i++
			}
			continue
		}
		if strings.HasPrefix(argLower, prefix) {
			if value != "" && !replaced {
				next = append(next, key+"="+value)
				replaced = true
			}
			continue
		}
		next = append(next, arg)
	}
	if value != "" && !replaced {
		next = append(next, key+"="+value)
	}
	return next
}

func hasAllLaunchFlags(args []string, flags []string) bool {
	for _, flag := range flags {
		if !hasLaunchFlag(args, flag) {
			return false
		}
	}
	return len(flags) > 0
}

func hasLaunchFlag(args []string, flag string) bool {
	for _, arg := range args {
		if strings.EqualFold(arg, flag) {
			return true
		}
	}
	return false
}

func setLaunchFlag(args []string, flag string, enabled bool) []string {
	next := make([]string, 0, len(args)+1)
	for _, arg := range args {
		if strings.EqualFold(arg, flag) {
			continue
		}
		next = append(next, arg)
	}
	if enabled {
		next = append(next, flag)
	}
	return next
}

func joinLaunchArgs(args []string) string {
	parts := make([]string, 0, len(args))
	for _, arg := range args {
		arg = strings.TrimSpace(arg)
		if arg == "" {
			continue
		}
		parts = append(parts, formatLaunchArg(arg))
	}
	return strings.Join(parts, " ")
}

func formatLaunchArg(arg string) string {
	if !strings.ContainsAny(arg, " \t\r\n") {
		return arg
	}
	if !strings.Contains(arg, `"`) {
		return `"` + arg + `"`
	}
	if !strings.Contains(arg, `'`) {
		return `'` + arg + `'`
	}
	return arg
}
