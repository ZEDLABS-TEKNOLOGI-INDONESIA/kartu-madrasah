package svg

import (
	"fmt"
	"os/exec"
)

const (
	KTPPortraitW  = 638
	KTPPortraitH  = 1012
	KTPLandscapeW = 1012
	KTPLandscapeH = 638
	DPI           = 300
	MaxWorkers    = 4
)

var sem = make(chan struct{}, MaxWorkers)

func RunInkscape(svgPath, pngPath string, portrait bool) error {
	sem <- struct{}{}
	defer func() { <-sem }()

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

	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("inkscape: %v - %s", err, out)
	}
	return nil
}
