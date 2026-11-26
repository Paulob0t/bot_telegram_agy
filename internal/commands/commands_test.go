package commands

import "testing"

func TestAgyWorkspaceIsExactFolder(t *testing.T) {
	uris := `["file:///home/paulobot"]`
	if !agyWorkspaceIs(uris, "/home/paulobot") {
		t.Fatal("misma carpeta debe coincidir")
	}
	if !agyWorkspaceIs(`["file:///home/paulobot/"]`, "/home/paulobot/") {
		t.Fatal("barra final debe coincidir")
	}
	if agyWorkspaceIs(`["file:///home/paulobot/Projects/x"]`, "/home/paulobot") {
		t.Fatal("subcarpeta no debe coincidir con la carpeta padre")
	}
}
