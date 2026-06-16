package svg

import (
	"fmt"
	"os/exec"
	"runtime"
)

const (
	KTPPortraitW  = 638
	KTPPortraitH  = 1012
	KTPLandscapeW = 1012
	KTPLandscapeH = 638
	DPI           = 300
)

var MaxWorkers = func() int {
	n := runtime.NumCPU()
	if n < 2 {
		return 2
	}
	if n > 16 {
		return 16
	}
	return n
}()

func RunInkscape(svgPath, pngPath string, portrait bool) error {
	w, h := KTPLandscapeW, KTPLandscapeH
	if portrait {
		w, h = KTPPortraitW, KTPPortraitH
	}

	cmd := exec.Command("inkscape",
		"--export-type=png",
		"--export-filename="+pngPath,
		fmt.Sprintf("--export-width=%d", w),
		fmt.Sprintf("--export-height=%d", h),
		fmt.Sprintf("--export-dpi=%d", DPI),
		svgPath,
	)

	cmd.Env = append(cmd.Environ(),
		"DBUS_SESSION_BUS_ADDRESS=",
		"DISPLAY=",
		"WAYLAND_DISPLAY=",
	)

	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("inkscape: %v - %s", err, out)
	}
	return nil
}
