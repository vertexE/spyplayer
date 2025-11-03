package main

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/exec"
	"time"

	"github.com/joho/godotenv"
	"github.com/zmb3/spotify/v2"
	"github.com/zmb3/spotify/v2/auth"
	"golang.org/x/oauth2"
)

func main() {
	authenticator := setupAuthenticator()
	startServer(authenticator)
}

func setupAuthenticator() *spotifyauth.Authenticator {
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	clientID := os.Getenv("SPOTIFY_ID")
	clientSecret := os.Getenv("SPOTIFY_SECRET")
	redirectURI := os.Getenv("SPOTIFY_REDIRECT")

	if clientID == "" || clientSecret == "" || redirectURI == "" {
		log.Fatal("Missing SPOTIFY_ID, SPOTIFY_SECRET, or SPOTIFY_REDIRECT environment variables")
	}

	authenticator := spotifyauth.New(
		spotifyauth.WithClientID(clientID),
		spotifyauth.WithClientSecret(clientSecret),
		spotifyauth.WithRedirectURL(redirectURI),
		spotifyauth.WithScopes(spotifyauth.ScopeUserReadCurrentlyPlaying, spotifyauth.ScopeUserReadPlaybackState),
	)
	return authenticator
}

// this sets up a temporary named pipe and then
// handles sending the data on regular cadence (every 5 seconds)
func startServer(authenticator *spotifyauth.Authenticator) {
	client, token := setupClient(authenticator)
	pipe, err := createTempNamedPipe()
	if err != nil {
		log.Fatalf("could not make named pipe: %v", err)
	}

	for {
		if token.Expiry.Before(time.Now()) {
			client, token = refreshTokenAndClient(authenticator, token)
		}

		details, err := fetchTrackDetails(client)
		if err != nil {
			log.Fatalf("unable to fetch track details: %v", err)
		}

		err = writeTrackDetailsToPipe(pipe, details)
		if err != nil {
			log.Fatalf("unable to write to named pipe: %v", err)
		}

		time.Sleep(time.Second * 5)
	}
}

// After fetching track details, write them to the named pipe.
// This function writes a formatted string to the pipe.
func writeTrackDetailsToPipe(pipePath string, details *TrackDetails) error {
	// Open the pipe for writing
	pipeFile, err := os.OpenFile(pipePath, os.O_WRONLY|os.O_SYNC, os.ModeNamedPipe)
	if err != nil {
		return err
	}
	defer pipeFile.Close()

	// Format the track details as a single line
	var output string
	if details != nil {
		output = fmt.Sprintf("Track: %s\nArtists: %s\nAlbum: %s\nProgress: %.2f\n", details.Name, details.Artists, details.Album, details.Progress)
	} else {
		output = "No track currently playing\n"
	}

	// Write to the pipe
	_, err = pipeFile.WriteString(output)
	return err
}

func refreshTokenAndClient(authenticator *spotifyauth.Authenticator, token *oauth2.Token) (*spotify.Client, *oauth2.Token) {
	authenticator.RefreshToken(context.Background(), token)
	httpClient := authenticator.Client(context.Background(), token)
	spotifyClient := spotify.New(httpClient)
	return spotifyClient, token
}

func setupClient(authenticator *spotifyauth.Authenticator) (*spotify.Client, *oauth2.Token) {
	// Get the auth URL and instruct the user to visit it
	spotifyAuthUrl := authenticator.AuthURL("state-token")
	fmt.Println("Please log in to Spotify by visiting the following page in your browser:", spotifyAuthUrl)

	// Wait for redirect with code (requires you to run a local web server or use the CLI)
	// For simplicity, we'll ask the user to paste the redirected URL here.
	fmt.Print("Paste the redirect URL here: ")
	var redirect string
	fmt.Scanln(&redirect)

	parsedURL, err := url.Parse(redirect)
	if err != nil {
		log.Fatalf("could not parse url %v", err)
	}
	code := parsedURL.Query().Get("code")

	token, err := authenticator.Exchange(context.Background(), code)
	if err != nil {
		log.Fatalf("could not get token: %v", err)
	}

	// Create client
	httpClient := authenticator.Client(context.Background(), token)
	spotifyClient := spotify.New(httpClient)
	return spotifyClient, token
}

type TrackDetails struct {
	Name     string
	Progress float64 // as a percent of how far we've gone through the track, between 0 and 1
	Artists  string  // comma separated
	Album    string
}

// fetches the current playing spotify track, or
// error if something went wrong
// returns nil if no track is playing
func fetchTrackDetails(client *spotify.Client) (*TrackDetails, error) {
	current, err := client.PlayerCurrentlyPlaying(context.Background())
	if err != nil {
		return nil, err // return error instead of fatal
	}

	if current != nil && current.Item != nil {
		// Calculate progress as a percent (0-1)
		var progress float64
		if current.Item.Duration > 0 {
			progress = float64(current.Progress) / float64(current.Item.Duration)
		}

		// Build comma-separated artist names
		artistNames := ""
		for i, artist := range current.Item.Artists {
			if i > 0 {
				artistNames += ", "
			}
			artistNames += artist.Name
		}

		track := &TrackDetails{
			Name:     current.Item.Name,
			Progress: progress,
			Artists:  artistNames,
			Album:    current.Item.Album.Name,
		}
		return track, nil
	}

	// No track currently playing
	return nil, nil
}

// createTempNamedPipe creates a temporary named pipe (FIFO) called "spyplayer" in the system's temp directory.
// Returns the full path to the FIFO, or an error if creation fails.
func createTempNamedPipe() (string, error) {
	tmpDir := os.TempDir()
	pipePath := tmpDir + "spyplayer"

	_ = os.Remove(pipePath)

	// Create the named pipe using mkfifo
	fmt.Println("creating named pipe")
	if err := exec.Command("mkfifo", pipePath).Run(); err != nil {
		return "", err
	}
	fmt.Printf("created pipe %s\n", pipePath)

	return pipePath, nil
}
