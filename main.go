package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/chromedp"
	"github.com/google/uuid"

	"github.com/jessevdk/go-flags"
)

var opts struct {
	Website   string  `short:"w" long:"website" description:"Website URL for famly app" default:"https://app.nfamilyclub.com/" env:"WEBSITE" required:"true"`
	Email     string  `short:"e" long:"email" description:"Email address for famly app login" env:"EMAIL" required:"true"`
	Password  string  `short:"p" long:"password" description:"Password for famly app login" env:"PASSWORD" required:"true"`
	ChildID   string  `long:"childid" description:"Child ID for child in famly app" env:"CHILDID" required:"true"`
	Latitude  float64 `long:"latitude" description:"Latitude to use for EXIF data" env:"LATITUDE"`
	Longitude float64 `long:"longitude" description:"Longitude to use for EXIF data" env:"LONGITUDE"`
}

func main() {
	_, err := flags.Parse(&opts)
	if err != nil {
		fmt.Println("Error parsing flags:", err)
		return
	}

	// Validate ChildID is a valid UUID
	if _, err := uuid.Parse(opts.ChildID); err != nil {
		fmt.Println("Invalid ChildID format. It should be a valid UUID:", err)
		return
	}

	// Connect to an existing Chrome instance
	allocatorCtx, cancel := chromedp.NewRemoteAllocator(context.Background(), "ws://localhost:9222/devtools/browser")
	defer cancel()

	ctx, cancelCtx := chromedp.NewContext(allocatorCtx)
	defer cancelCtx()

	var pageTitle string
	var emailInputFound bool

	err = chromedp.Run(ctx,
		chromedp.Navigate(opts.Website),
		chromedp.Sleep(5*time.Second),

		// Try to find the email input, but don't fail if not found
		chromedp.ActionFunc(func(ctx context.Context) error {
			var nodes []*cdp.Node
			err := chromedp.Nodes(`input[type="email"]`, &nodes, chromedp.AtLeast(0)).Do(ctx)
			emailInputFound = len(nodes) > 0
			return err
		}),
	)
	if err != nil {
		fmt.Println("Error navigating:", err)
		return
	}

	if emailInputFound {
		// Perform login
		err = chromedp.Run(ctx,
			chromedp.SendKeys(`input[type="email"]`, opts.Email, chromedp.ByQuery),
			chromedp.SendKeys(`input[type="password"]`, opts.Password, chromedp.ByQuery),
			chromedp.Click(`button[type="submit"]`, chromedp.ByQuery),
			chromedp.Sleep(3*time.Second),
			chromedp.Title(&pageTitle),
		)
		if err != nil {
			fmt.Println("Error during login:", err)
			return
		}
		fmt.Println("Logged in! Page Title:", pageTitle)
	} else {
		// Already logged in
		err = chromedp.Run(ctx, chromedp.Title(&pageTitle))
		fmt.Println("Already logged in! Page Title:", pageTitle)

		if err != nil {
			fmt.Println("Error checking page title:", err)
			return
		}
	}

	// Read famly.accessToken from localStorage
	var accessToken string
	var installationId string
	err = chromedp.Run(ctx,
		chromedp.EvaluateAsDevTools(`window.localStorage.getItem("famly.accessToken")`, &accessToken),
		chromedp.EvaluateAsDevTools(`window.localStorage.getItem("famly.installationId")`, &installationId),
		chromedp.Sleep(1*time.Second),
		chromedp.ActionFunc(func(ctx context.Context) error {
			if accessToken == "" {
				return fmt.Errorf("famly.accessToken not found in localStorage")
			}
			if installationId == "" {
				return fmt.Errorf("famly.installationid not found in localStorage")
			}
			return nil
		}),
	)
	if err != nil {
		fmt.Println("Error reading famly data:", err)
		return
	}
	accessToken = strings.ReplaceAll(accessToken, `"`, "")
	fmt.Printf("famly.accessToken: '%s'\n", accessToken)
	installationId = strings.ReplaceAll(installationId, `"`, "")
	fmt.Printf("famly.installationId: '%s'\n", installationId)

	// Download all images using pagination with olderThan
	var (
		oldest time.Time

		page   = 1
		client = &http.Client{}
	)
	for {
		// Build request
		req, err := http.NewRequest("GET", opts.Website+"api/v2/images/tagged", nil)
		if err != nil {
			fmt.Println("Error creating request:", err)
			return
		}
		q := req.URL.Query()
		q.Set("childId", opts.ChildID)
		q.Set("limit", "100")
		if !oldest.IsZero() {
			// Truncate the time to the start of the day as there seems to be photos with the exact
			// same created time. I'm guessing internally famly use the upload time as createdAt.
			// It's also not a big deal as we don't redownload images that already exist.
			q.Set("olderThan", oldest.Truncate(24*time.Hour).Format("2006-01-02T15:04:05-07:00"))
		}
		req.URL.RawQuery = q.Encode()
		req.Header.Set("x-famly-accesstoken", accessToken)
		req.Header.Set("x-famly-installationid", installationId)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("accept", "application/json")
		req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/137.0.0.0 Safari/537.36")

		resp, err := client.Do(req)
		if err != nil {
			fmt.Println("Error making GET request:", err)
			return
		}

		if resp.StatusCode != http.StatusOK {
			fmt.Printf("Failed to fetch images: %s\n", resp.Status)
			resp.Body.Close()
			return
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			fmt.Println("Error reading response body:", err)
			return
		}

		var taggedImages TaggedImages
		err = json.Unmarshal(body, &taggedImages)
		if err != nil {
			fmt.Println("Error unmarshalling JSON response:", err)
			return
		}
		if len(taggedImages) == 0 {
			fmt.Println("No more images to download.")
			break
		}
		fmt.Printf("Page %d: Downloading %d images...\n", page, len(taggedImages))

		// Download images
		for _, img := range taggedImages {
			if img.CreatedAt.Before(oldest) || oldest.IsZero() {
				oldest = img.CreatedAt
			}

			// skip if image already exists
			if _, err := os.Stat(fmt.Sprintf("output/%s-%s.jpg", img.CreatedAt.Format(time.DateOnly), img.ImageID)); err == nil {
				fmt.Printf("Image %s already exists, skipping...\n", img.ImageID)
				continue
			}

			fullSizedImageUrl := fmt.Sprintf("%s/%dx%d/%s", img.Prefix, img.Width, img.Height, img.Key)
			resp, err := http.Get(fullSizedImageUrl)
			if err != nil {
				fmt.Println("Error downloading image:", err)
				continue
			}
			if resp.StatusCode != http.StatusOK {
				fmt.Printf("Failed to download image %s: %s\n", img.ImageID, resp.Status)
				resp.Body.Close()
				continue
			}
			if _, err := os.Stat("output"); os.IsNotExist(err) {
				err = os.Mkdir("output", 0755)
				if err != nil {
					fmt.Println("Error creating output directory:", err)
					resp.Body.Close()
					continue
				}
			}
			fileName := fmt.Sprintf("output/%s-%s.jpg", img.CreatedAt.Format(time.DateOnly), img.ImageID)
			outFile, err := os.Create(fileName)
			if err != nil {
				fmt.Println("Error creating output file:", err)
				resp.Body.Close()
				continue
			}
			_, err = io.Copy(outFile, resp.Body)
			resp.Body.Close()
			outFile.Close()
			if err != nil {
				fmt.Println("Error saving image to file:", err)
			}

			// Update EXIF data with createdAt and location using go-exif
			err = updateExifData(fileName, img.CreatedAt, opts.Latitude, opts.Longitude)
			if err != nil {
				fmt.Println("Error writing EXIF data to file:", err)
				continue
			}

			fmt.Printf("Image %s saved to %s\n", img.ImageID, fileName)
		}

		page++
	}

	err = chromedp.Run(ctx,
		chromedp.Sleep(10*time.Second),
	)
	if err != nil {
		fmt.Println("Error during final wait:", err)
		return
	}
	fmt.Println("All done here.")
}

// updateExifData updates the EXIF DateTimeOriginal and GPS tags in a JPEG file.
func updateExifData(imagePath string, createdAt time.Time, latitude, longitude float64) error {
	// Check if exiftool is available
	if _, err := exec.LookPath("exiftool"); err != nil {
		return fmt.Errorf("exiftool not found: %w", err)
	}

	// Format datetime for EXIF (YYYY:MM:DD HH:MM:SS)
	dateTimeStr := createdAt.Format("2006:01:02 15:04:05")

	// Convert coordinates to EXIF format
	latRef := "N"
	lat := latitude
	if lat < 0 {
		latRef = "S"
		lat = -lat
	}

	lonRef := "E"
	lon := longitude
	if lon < 0 {
		lonRef = "W"
		lon = -lon
	}

	// Build exiftool command
	cmd := exec.Command("exiftool",
		"-overwrite_original",
		"-DateTimeOriginal="+dateTimeStr,
		"-CreateDate="+dateTimeStr,
		"-ModifyDate="+dateTimeStr,
		fmt.Sprintf("-GPSLatitude=%f", lat),
		"-GPSLatitudeRef="+latRef,
		fmt.Sprintf("-GPSLongitude=%f", lon),
		"-GPSLongitudeRef="+lonRef,
		imagePath,
	)

	// Execute command
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("exiftool failed: %w, output: %s", err, string(output))
	}

	return nil

}
