# Famly Image Download

A script to automate downloading of images of your child from a [famly](https://www.famly.co/) based nursary site. It does so by downloading the images you've tagged them in.

I've used it for [nFamily](https://nfamilyclub.com) to download images from [app.nfamilyclub.com](https://app.nfamilyclub.com/). But it probably works for other nurseries using Famly's platform.

It was hacked together in an evening, so quality is almost nonexistant. It looks like famly are transitioning to a graphql API. This tool makes use of the old API, so will likely break before too long.

This tool has no association with nFamily or Famly. As per the licence it is without warranty.

## Features

- Logs in to app.nfamilyclub.com using your credentials
- Downloads all tagged images for a given child (with pagination)
- Skips images that already exist locally
- Sets EXIF `DateTimeOriginal` and GPS location on downloaded images using `exiftool`
- Stores images in the `output/` directory

## Requirements

- Go 1.24+
- [exiftool](https://exiftool.org/) installed and available in your `PATH`
- A running Chrome/Chromeium instance with remote debugging enabled (see below)

## Setup

**Clone the repository**

```sh
git clone https://github.com/steakunderscore/famly-image-download.git
cd famly-image-download
```

## Usage

Start Chrome with remote debugging and run

```sh
google-chrome --remote-debugging-port=9222
go run ./*.go --email you@example.com --password yourpassword --childid <child-id> --latitude <lat> --longitude <lon>
```

On MacOS, you'll need to run: 
```sh
/Applications/Google\ Chrome.app/Contents/MacOS/Google\ Chrome --remote-debugging-port=9222 --user-data-dir=./chrome_data/
go run ./*.go --email you@example.com --password yourpassword --childid <child-id> --latitude <lat> --longitude <lon>
```

The `childid` you can take from the URL when viewing your child's profile/activity page, for example `https://app.nfamilyclub.com/#/account/childProfile/afacb3b1-054a-4da3-9220-a1bfa85ec28c/activity`.


## Output

Downloaded images will be saved in the `output/` directory, named as:

```
YYYY-MM-DD-<imageId>.jpg
```

EXIF metadata will be set for date/time and GPS location.

## Issues

Originally I thought I'd have to do more webscraping, so I began with using chromedp. Half way through I realised I could just call one of their API endpoints and so switched to just doing it in Go. Ideally chromedp should get removed, but using it meant I didn't have to understand their login and token generation process.

## Troubleshooting

 * Make sure `exiftool` is installed and in your `PATH`.
 * Ensure Chrome is running with remote debugging enabled on port 9222.
 * If you see errors about missing modules, run `go mod tidy`.

## License

Famly Image Download is released under the [MIT License](LICENSE).
