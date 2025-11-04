# spyplayer 

spyplayer allows you to very easily interact with the spotify API via named file pipes.
Example usage can be found in my nvim config, where I run this tool in the background
to show the track name + use basic controls.

## General Idea

spyplayer will
1. create 2 files, `/tmp/spyplayer-control` and `/tmp/spyplayer-track`
2. uses mkfifo to turn these into FIFO pipes
3. manages 2 threads:
    a. *-control will listen for any user actions, currently supported: play, pause, next
    b. *-track writes every 3 seconds, in the format `<track-name> - <artists-comma-separated>`
