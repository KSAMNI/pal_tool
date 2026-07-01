package app

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestCurrentServerStatusReportsPortDiagnostics(t *testing.T) {
	panel, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer panel.Close()

	serverPath := t.TempDir()
	writeFakePalServerBinary(t, serverPath)
	gamePort, closeGame := occupyUDPPort(t)
	defer closeGame()
	restPort, closeREST := occupyTCPPort(t)
	defer closeREST()
	rconPort := freeTCPPort(t)
	writePortTestConfig(t, serverPath, true, restPort, true, rconPort)

	status := panel.currentServerStatus(settingsPayload{
		PalServerPath:    serverPath,
		ServerLaunchArgs: fmt.Sprintf("-port=%d", gamePort),
	})
	checks := portCheckMap(status.PortChecks)
	if got := checks["Game/udp"]; got.Port != gamePort || got.Status != portStatusInUse || got.Source != "server_launch_args" {
		t.Fatalf("game port check = %#v, want in-use launch arg port %d", got, gamePort)
	}
	if got := checks["REST API/tcp"]; got.Port != restPort || got.Status != portStatusInUse || got.Source != "PalWorldSettings.ini" {
		t.Fatalf("REST port check = %#v, want in-use config port %d", got, restPort)
	}
	if got := checks["RCON/tcp"]; got.Port != rconPort || got.Status != portStatusAvailable || got.Source != "PalWorldSettings.ini" {
		t.Fatalf("RCON port check = %#v, want available config port %d", got, rconPort)
	}
}

func TestPortDiagnosticsReportDisabledConfigPorts(t *testing.T) {
	panel, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer panel.Close()

	serverPath := t.TempDir()
	writeFakePalServerBinary(t, serverPath)
	writePortTestConfig(t, serverPath, false, 18212, false, 25575)

	status := panel.currentServerStatus(settingsPayload{PalServerPath: serverPath})
	checks := portCheckMap(status.PortChecks)
	if got := checks["REST API/tcp"]; got.Port != 18212 || got.Status != portStatusDisabled {
		t.Fatalf("REST disabled port check = %#v", got)
	}
	if got := checks["RCON/tcp"]; got.Port != 25575 || got.Status != portStatusDisabled {
		t.Fatalf("RCON disabled port check = %#v", got)
	}
}

func TestPortDiagnosticsUseRestAPIURLWhenConfigIsMissing(t *testing.T) {
	panel, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer panel.Close()

	status := panel.currentServerStatus(settingsPayload{
		RestAPIURL: "http://127.0.0.1:19212/v1/api",
	})
	checks := portCheckMap(status.PortChecks)
	if got := checks["REST API/tcp"]; got.Port != 19212 || got.Source != "rest_api_url" {
		t.Fatalf("REST URL fallback port check = %#v", got)
	}
	if got := checks["RCON/tcp"]; got.Port != 25575 || got.Status != portStatusUnknown {
		t.Fatalf("RCON missing-config port check = %#v", got)
	}
}

func writePortTestConfig(t *testing.T, serverPath string, restEnabled bool, restPort int, rconEnabled bool, rconPort int) {
	t.Helper()
	configPath, _, _, err := palConfigPaths(serverPath)
	if err != nil {
		t.Fatalf("palConfigPaths() error = %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	content := fmt.Sprintf(`[/Script/Pal.PalGameWorldSettings]
OptionSettings=(RESTAPIEnabled=%s,RESTAPIPort=%d,RCONEnabled=%s,RCONPort=%d)
`, palBool(restEnabled), restPort, palBool(rconEnabled), rconPort)
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

func palBool(value bool) string {
	if value {
		return "True"
	}
	return "False"
}

func occupyTCPPort(t *testing.T) (int, func()) {
	t.Helper()
	listener, err := net.Listen("tcp4", "0.0.0.0:0")
	if err != nil {
		t.Fatalf("listen tcp: %v", err)
	}
	return listener.Addr().(*net.TCPAddr).Port, func() { _ = listener.Close() }
}

func occupyUDPPort(t *testing.T) (int, func()) {
	t.Helper()
	conn, err := net.ListenPacket("udp4", "0.0.0.0:0")
	if err != nil {
		t.Fatalf("listen udp: %v", err)
	}
	return conn.LocalAddr().(*net.UDPAddr).Port, func() { _ = conn.Close() }
}

func freeTCPPort(t *testing.T) int {
	t.Helper()
	port, closePort := occupyTCPPort(t)
	closePort()
	return port
}

func portCheckMap(checks []portCheck) map[string]portCheck {
	ret := make(map[string]portCheck, len(checks))
	for _, check := range checks {
		ret[check.Name+"/"+check.Protocol] = check
	}
	return ret
}
