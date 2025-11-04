package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/zmb3/spotify/v2"
	"github.com/zmb3/spotify/v2/auth"
	"golang.org/x/oauth2"
)

type FifoPipeName string

const (
	pipeTrackDetails FifoPipeName = "fifoplayer-track"
	pipeTrackControl FifoPipeName = "fifoplayer-control"
)

type TrackControlAction string

const (
	Play  TrackControlAction = "play"
	Pause TrackControlAction = "pause"
	Next  TrackControlAction = "next"
)

func main() {
	authenticator := setupAuthenticator()
	startFifoPipes(authenticator)
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
		spotifyauth.WithScopes(
			spotifyauth.ScopeUserReadCurrentlyPlaying,
			spotifyauth.ScopeUserReadPlaybackState,
			spotifyauth.ScopeUserModifyPlaybackState,
		),
	)
	return authenticator
}

// this sets up a temporary named pipe and then
// handles sending the data on regular cadence (every 5 seconds)
func startFifoPipes(authenticator *spotifyauth.Authenticator) {
	client, token := setupClient(authenticator)

	go func() {
		pipe, err := createTempNamedPipe(pipeTrackDetails)
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

			time.Sleep(time.Second * 3)
		}
	}()

	pipe, err := createTempNamedPipe(pipeTrackControl)
	if err != nil {
		log.Fatalf("could not make named pipe: %v", err)
	}

	for {
		action, err := readTrackControlFromPipe(pipe)
		if err != nil {
			log.Fatalf("unable to fetch track details: %v", err)
		}

		if token.Expiry.Before(time.Now()) {
			client, token = refreshTokenAndClient(authenticator, token)
		}

		switch action {
		case Play:
			if err := client.Play(context.Background()); err != nil {
				log.Printf("failed to play track: %v\n", err)
			} else {
				log.Println("playing track")
			}
		case Pause:
			if err := client.Pause(context.Background()); err != nil {
				log.Printf("failed to pause track: %v\n", err)
			} else {
				log.Println("paused track")
			}
		case Next:
			if err := client.Next(context.Background()); err != nil {
				log.Printf("failed to skip track: %v\n", err)
			} else {
				log.Println("skipped track")
			}
		}
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
		output = fmt.Sprintf("%s - %s", details.Name, details.Artists)
	} else {
		output = "No track currently playing"
	}

	// Write to the pipe
	_, err = pipeFile.WriteString(output)
	return err
}

func readTrackControlFromPipe(pipePath string) (TrackControlAction, error) {
	// Open the pipe for reading
	pipeFile, err := os.OpenFile(pipePath, os.O_RDONLY|os.O_SYNC, os.ModeNamedPipe)
	if err != nil {
		return "", err
	}
	defer pipeFile.Close()

	// Read up to 128 bytes from the pipe (enough for a simple action string)
	buf := make([]byte, 128)
	n, err := pipeFile.Read(buf)
	if err != nil {
		return "", err
	}

	// Trim whitespace and convert to TrackControlAction
	action := TrackControlAction(strings.TrimSpace(string(buf[:n])))
	return action, nil
}
func refreshTokenAndClient(authenticator *spotifyauth.Authenticator, token *oauth2.Token) (*spotify.Client, *oauth2.Token) {
	authenticator.RefreshToken(context.Background(), token)
	httpClient := authenticator.Client(context.Background(), token)
	spotifyClient := spotify.New(httpClient)
	return spotifyClient, token
}

func setupClient(authenticator *spotifyauth.Authenticator) (*spotify.Client, *oauth2.Token) {
	spotifyAuthUrl := authenticator.AuthURL("state-token")
	if err := exec.Command("open", spotifyAuthUrl).Run(); err != nil {
		log.Fatalf("could not open the spotifyAuthUrl")
	}

	// Channel to receive the code from the HTTP handler
	codeCh := make(chan string)

	// Start local HTTP server for callback
	srv := &http.Server{Addr: ":8080"}
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		if code == "" {
			http.Error(w, "No code in callback", http.StatusBadRequest)
			return
		}
		fmt.Fprintf(w, "Login successful! You may close this window.")
		codeCh <- code
	})

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()

	// Wait for code from callback
	code := <-codeCh

	// Shutdown server after receiving code
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()
	_ = srv.Shutdown(ctx)

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

// createTempNamedPipe creates a temporary named pipe (FIFO) in the system's temp directory.
// Returns the full path to the FIFO, or an error if creation fails.
func createTempNamedPipe(name FifoPipeName) (string, error) {
	pipePath := fmt.Sprintf("/tmp/%s", name)
	_ = os.Remove(pipePath)

	// Create the named pipe using mkfifo
	log.Println("creating named pipe")
	if err := exec.Command("mkfifo", pipePath).Run(); err != nil {
		return "", err
	}
	log.Printf("created pipe %s\n", pipePath)

	return pipePath, nil
}
