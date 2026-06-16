package worker

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"kartu-madrasah/csvutil"
	"kartu-madrasah/job"
	"kartu-madrasah/svg"
)

const JobWorkers = 4

type Worker struct {
	Store *job.Store
	Queue *job.Queue
}

func New(store *job.Store, queue *job.Queue) *Worker {
	return &Worker{Store: store, Queue: queue}
}

func (w *Worker) Start(ctx context.Context) {
	for i := 0; i < JobWorkers; i++ {
		go w.loop(ctx)
	}
}

func (w *Worker) loop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		payload, err := w.Queue.Pop(ctx)
		if err != nil || payload == nil {
			continue
		}
		w.process(ctx, payload)
	}
}

func (w *Worker) process(ctx context.Context, p *job.QueuePayload) {
	defer func() {
		if rec := recover(); rec != nil {
			w.Store.SetStatus(ctx, p.JobID, "error")
			w.Store.IncrFailed(ctx, p.JobID, fmt.Sprintf("panic: %v", rec))
			os.RemoveAll(p.TmpDir)
		}
	}()

	w.Store.SetStatus(ctx, p.JobID, "processing")

	inputDir := filepath.Join(p.TmpDir, "input")
	outputDir := filepath.Join(p.TmpDir, "output")
	os.MkdirAll(outputDir, 0755)

	svgData, err := os.ReadFile(filepath.Join(inputDir, "desain.svg"))
	if err != nil {
		w.Store.SetStatus(ctx, p.JobID, "error")
		w.Store.IncrFailed(ctx, p.JobID, "gagal baca SVG: "+err.Error())
		os.RemoveAll(p.TmpDir)
		return
	}

	csvData, err := os.ReadFile(filepath.Join(inputDir, "data.csv"))
	if err != nil {
		w.Store.SetStatus(ctx, p.JobID, "error")
		w.Store.IncrFailed(ctx, p.JobID, "gagal baca CSV: "+err.Error())
		os.RemoveAll(p.TmpDir)
		return
	}

	rows, _, err := csvutil.Parse(csvData)
	if err != nil {
		w.Store.SetStatus(ctx, p.JobID, "error")
		w.Store.IncrFailed(ctx, p.JobID, "gagal parse CSV: "+err.Error())
		os.RemoveAll(p.TmpDir)
		return
	}

	globalImages := map[string][]byte{}
	if data, err := os.ReadFile(filepath.Join(inputDir, "logo_instansi")); err == nil {
		globalImages["logo_instansi"] = data
	}

	fotoMap := readDirImages(filepath.Join(inputDir, "foto"))
	qrMap := readDirImages(filepath.Join(inputDir, "qr"))

	tpl := svg.Compile(svgData)
	globalURIs := svg.EncodeImages(globalImages)

	uidCount := make(map[string]int, len(rows))
	for _, row := range rows {
		uidCount[baseUID(row)]++
	}
	uidSeen := make(map[string]int, len(rows))

	type prepared struct {
		uid     string
		svgPath string
		pngPath string
	}

	var preparedList []prepared

	for i, row := range rows {
		uid := resolveUID(row, i)
		base := baseUID(row)

		var fileUID string
		if uidCount[base] > 1 {
			fileUID = fmt.Sprintf("%s_%d", base, uidSeen[base])
			uidSeen[base]++
		} else {
			fileUID = base
		}

		rowImages := map[string][]byte{}
		if data := findImage(fotoMap, row["foto_user"], row["username"]); data != nil {
			rowImages["foto_user"] = data
		}
		if data := findImage(qrMap, row["qr_user"], row["username"]); data != nil {
			rowImages["qr_user"] = data
		}

		injected, err := tpl.Inject(row, globalURIs, rowImages)
		if err != nil {
			w.Store.IncrFailed(ctx, p.JobID, fmt.Sprintf("[%s] gagal inject SVG: %v", uid, err))
			continue
		}

		svgPath := filepath.Join(outputDir, fileUID+".svg")
		pngPath := filepath.Join(outputDir, fileUID+".png")

		if err := os.WriteFile(svgPath, injected, 0644); err != nil {
			w.Store.IncrFailed(ctx, p.JobID, fmt.Sprintf("[%s] gagal tulis SVG: %v", uid, err))
			continue
		}

		preparedList = append(preparedList, prepared{uid, svgPath, pngPath})
	}

	var wg sync.WaitGroup

	for _, item := range preparedList {
		item := item
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := svg.RunInkscape(item.svgPath, item.pngPath, p.Portrait); err != nil {
				w.Store.IncrFailed(ctx, p.JobID, fmt.Sprintf("[%s] gagal render: %v", item.uid, err))
			} else {
				w.Store.IncrDone(ctx, p.JobID)
			}
			os.Remove(item.svgPath)
		}()
	}
	wg.Wait()

	var outFiles []string
	for _, item := range preparedList {
		if _, err := os.Stat(item.pngPath); err == nil {
			outFiles = append(outFiles, item.pngPath)
		}
	}

	os.RemoveAll(inputDir)

	jd, _ := w.Store.Get(ctx, p.JobID)
	status := "done"
	if jd != nil && jd.Failed > 0 && len(outFiles) == 0 {
		status = "error"
	}

	w.Store.Finish(ctx, p.JobID, status, len(outFiles), outFiles)

	go func() {
		time.Sleep(job.JobTTL)
		os.RemoveAll(p.TmpDir)
	}()
}

func baseUID(row map[string]string) string {
	if u := row["username"]; u != "" {
		return u
	}
	if u := row["uid_kartu"]; u != "" {
		return u
	}
	return "row"
}

func resolveUID(row map[string]string, idx int) string {
	if u := row["username"]; u != "" {
		return u
	}
	if u := row["uid_kartu"]; u != "" {
		return u
	}
	return fmt.Sprintf("row_%d", idx)
}

func readDirImages(dir string) map[string][]byte {
	result := map[string][]byte{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return result
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		ext := filepath.Ext(name)
		result[name] = data
		if ext != "" {
			result[name[:len(name)-len(ext)]] = data
		}
	}
	return result
}

func findImage(m map[string][]byte, pathInCSV, username string) []byte {
	if d, ok := m[filepath.Base(pathInCSV)]; ok {
		return d
	}
	if d, ok := m[pathInCSV]; ok {
		return d
	}
	for _, ext := range []string{".png", ".jpg", ".jpeg", ".webp"} {
		if d, ok := m[username+ext]; ok {
			return d
		}
	}
	if d, ok := m[username]; ok {
		return d
	}
	return nil
}
