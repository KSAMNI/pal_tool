package app

import (
	"net"
	"net/url"
	"strconv"
	"strings"
)

const (
	portStatusAvailable = "available"
	portStatusInUse     = "in_use"
	portStatusDisabled  = "disabled"
	portStatusInvalid   = "invalid"
	portStatusUnknown   = "unknown"
)

func (a *App) currentPortChecks(settings settingsPayload) []portCheck {
	launchSettings := settings
	applyLaunchArgSummary(&launchSettings)

	gamePort := launchSettings.GamePort
	gameSource := "server_launch_args"
	if gamePort == 0 {
		gamePort = 8211
		gameSource = "default"
	}
	checks := []portCheck{
		probePortCheck("Game", "udp", gamePort, gameSource, true),
	}

	configValues, configFound, configErr := existingPalConfigValues(settings.PalServerPath)
	if configErr != nil {
		checks = append(checks,
			unknownPortCheck("REST API", "tcp", 0, "PalWorldSettings.ini", configErr.Error()),
			unknownPortCheck("RCON", "tcp", 0, "PalWorldSettings.ini", configErr.Error()),
		)
		return checks
	}
	if configFound {
		checks = append(checks,
			probePortCheck("REST API", "tcp", configValues.RESTAPIPort, "PalWorldSettings.ini", configValues.RESTAPIEnabled),
			probePortCheck("RCON", "tcp", configValues.RCONPort, "PalWorldSettings.ini", configValues.RCONEnabled),
		)
		return checks
	}

	restPort := restAPIPortFromURL(settings.RestAPIURL)
	if restPort > 0 {
		checks = append(checks, probePortCheck("REST API", "tcp", restPort, "rest_api_url", true))
	} else {
		checks = append(checks, unknownPortCheck("REST API", "tcp", 8212, "default", "PalWorldSettings.ini was not found"))
	}
	checks = append(checks, unknownPortCheck("RCON", "tcp", 25575, "default", "PalWorldSettings.ini was not found"))
	return checks
}

func existingPalConfigValues(serverPath string) (palConfigValues, bool, error) {
	serverPath = strings.TrimSpace(serverPath)
	if serverPath == "" {
		return palConfigValues{}, false, nil
	}
	configPath, defaultPath, platform, err := palConfigPaths(serverPath)
	if err != nil {
		return palConfigValues{}, false, err
	}
	if !fileExists(configPath) {
		return palConfigValues{}, false, nil
	}
	doc, err := readPalConfigDocument(configPath, defaultPath, platform)
	if err != nil {
		return palConfigValues{}, false, err
	}
	return doc.Values, true, nil
}

func probePortCheck(name, protocol string, port int, source string, enabled bool) portCheck {
	check := portCheck{
		Name:     name,
		Protocol: protocol,
		Port:     port,
		Source:   source,
	}
	if !enabled {
		check.Status = portStatusDisabled
		check.Message = "disabled in PalWorldSettings.ini"
		return check
	}
	if port < 1 || port > 65535 {
		check.Status = portStatusInvalid
		check.Message = "port must be between 1 and 65535"
		return check
	}
	status, message := probeLocalPort(protocol, port)
	check.Status = status
	check.Message = message
	return check
}

func unknownPortCheck(name, protocol string, port int, source, message string) portCheck {
	return portCheck{
		Name:     name,
		Protocol: protocol,
		Port:     port,
		Source:   source,
		Status:   portStatusUnknown,
		Message:  message,
	}
}

func probeLocalPort(protocol string, port int) (string, string) {
	addr := net.JoinHostPort("0.0.0.0", strconv.Itoa(port))
	switch strings.ToLower(protocol) {
	case "tcp":
		listener, err := net.Listen("tcp4", addr)
		if err != nil {
			return portProbeErrorStatus(err)
		}
		_ = listener.Close()
		return portStatusAvailable, ""
	case "udp":
		conn, err := net.ListenPacket("udp4", addr)
		if err != nil {
			return portProbeErrorStatus(err)
		}
		_ = conn.Close()
		return portStatusAvailable, ""
	default:
		return portStatusUnknown, "unsupported protocol: " + protocol
	}
}

func portProbeErrorStatus(err error) (string, string) {
	message := err.Error()
	lower := strings.ToLower(message)
	if strings.Contains(lower, "address already in use") ||
		strings.Contains(lower, "only one usage of each socket address") ||
		strings.Contains(lower, "bind: an attempt was made to access a socket in a way forbidden") {
		return portStatusInUse, "port is already in use"
	}
	return portStatusUnknown, message
}

func restAPIPortFromURL(raw string) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	candidates := []string{raw}
	if !strings.Contains(raw, "://") {
		candidates = append(candidates, "http://"+raw)
	}
	for _, candidate := range candidates {
		parsed, err := url.Parse(candidate)
		if err != nil {
			continue
		}
		if portText := parsed.Port(); portText != "" {
			port, err := strconv.Atoi(portText)
			if err == nil {
				return port
			}
		}
	}
	return 0
}
