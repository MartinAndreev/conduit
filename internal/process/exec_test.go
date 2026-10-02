package process

import "testing"

func TestExecLauncherRejectsEmptyPath(t *testing.T) {
	if _, err := (ExecLauncher{}).Start(Spec{}); err == nil {
		t.Fatal("Start accepted an empty executable path")
	}
}
