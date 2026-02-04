package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/browser"
	"github.com/chromedp/chromedp"
)

const (
	MaxID      = 1025
	NumWorkers = 4
)

func main() {
	absAbout, _ := filepath.Abs("./about")
	absStats, _ := filepath.Abs("./stats")

	fmt.Printf("Starting Scraper with %d workers...\n", NumWorkers)
	fmt.Println("---------------------------------------------------------")

	parentCtx, parentCancel := context.WithCancel(context.Background())
	defer parentCancel()

	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.NoSandbox,
		chromedp.Headless,
	)
	allocCtx, _ := chromedp.NewExecAllocator(parentCtx, opts...)

	jobs := make(chan int, MaxID)
	var wg sync.WaitGroup

	// Start Workers
	for w := 1; w <= NumWorkers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			ctx, cancel := chromedp.NewContext(allocCtx)
			defer cancel()

			// Warm up the browser with a blank page
			if err := chromedp.Run(ctx, chromedp.Navigate("about:blank")); err != nil {
				fmt.Printf("Worker %d failed to start: %v\n", workerID, err)
				return
			}

			for id := range jobs {
				processID(ctx, id, absAbout, absStats)
			}
		}(w)
	}

	// Feed IDs
	for i := 1; i <= MaxID; i++ {
		jobs <- i
	}
	close(jobs)

	wg.Wait()
	fmt.Println("\n All workers finished successfully!")
}

func processID(ctx context.Context, id int, absAbout, absStats string) {
	paddedID := fmt.Sprintf("%04d", id)
	genFolder := getGenFolder(id)

	for _, mode := range []string{"about", "stats"} {
		rootDir := absAbout
		if mode == "stats" {
			rootDir = absStats
		}

		targetDir := filepath.Join(rootDir, genFolder)
		os.MkdirAll(targetDir, 0755)

		if exists(targetDir, paddedID) {
			continue 
		}

		fmt.Printf("Worker processing ID %s [%s]\n", paddedID, mode)
		
		// Run task with a local timeout per Pokemon so a hang doesn't kill the worker
		taskCtx, taskCancel := context.WithTimeout(ctx, 45*time.Second)
		err := runDownload(taskCtx, id, mode, targetDir)
		taskCancel()

		if err != nil {
			fmt.Printf("Error ID %s [%s]: %v\n", paddedID, mode, err)
			// Cooldown on error for API
			time.Sleep(2 * time.Second)
		}
	}
}

func getGenFolder(id int) string {
	switch {
	case id <= 151: return "Gen_1_Kanto"
	case id <= 251: return "Gen_2_Johto"
	case id <= 386: return "Gen_3_Hoenn"
	case id <= 493: return "Gen_4_Sinnoh"
	case id <= 649: return "Gen_5_Unova"
	case id <= 721: return "Gen_6_Kalos"
	case id <= 809: return "Gen_7_Alola"
	case id <= 905: return "Gen_8_Galar"
	default: return "Gen_9_Paldea"
	}
}

func exists(dir, idPrefix string) bool {
	files, _ := os.ReadDir(dir)
	for _, f := range files {
		if strings.HasPrefix(f.Name(), "pokedex_"+idPrefix) {
			return true
		}
	}
	return false
}

func runDownload(ctx context.Context, id int, mode string, downloadPath string) error {
	var name string
	paddedID := fmt.Sprintf("%04d", id)

	err := chromedp.Run(ctx,
		browser.SetDownloadBehavior(browser.SetDownloadBehaviorBehaviorAllow).
			WithDownloadPath(downloadPath).
			WithEventsEnabled(true),
		chromedp.Navigate(fmt.Sprintf("https://basvanderploeg.nl/xteink/pokemon/?id=%d", id)),
		chromedp.Evaluate(fmt.Sprintf(`
			document.getElementById('viewMode').value = '%s';
			document.getElementById('pokeSearch').value = '%d';
			fetchData();
		`, mode, id), nil),
		chromedp.Poll(fmt.Sprintf(`document.getElementById('disp-num').innerText === "%s"`, paddedID), nil),
		chromedp.Text(`#disp-name`, &name, chromedp.ByID),
		chromedp.Click(`.btn-download`, chromedp.ByQuery),
		chromedp.Sleep(3 * time.Second),
	)

	if err != nil {
		return err
	}

	oldFile := filepath.Join(downloadPath, fmt.Sprintf("pokedex_%s.bmp", name))
	newFile := filepath.Join(downloadPath, fmt.Sprintf("pokedex_%s_%s.bmp", paddedID, strings.ToUpper(name)))

	for retry := 0; retry < 15; retry++ {
		if _, err := os.Stat(oldFile); err == nil {
			return os.Rename(oldFile, newFile)
		}
		time.Sleep(1 * time.Second)
	}
	return fmt.Errorf("file %s timeout", name)
}
