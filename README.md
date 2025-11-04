# fifoplayer 

fifoplayer allows you to very easily interact with the spotify API via named file pipes.
Example usage can be found in my nvim config, where I run this tool in the background
to show the track name + use basic controls.

## General Idea

fifoplayer will
1. authenticates via localhost callback, you will need to fill in `.env` with the correct values, refer to `.env.example`
2. create 2 files, `/tmp/fifoplayer-control` and `/tmp/fifoplayer-track`
3. uses mkfifo to turn these into FIFO pipes
4. manages 2 threads:
    a. *-control will listen for any user actions, currently supported: play, pause, next
    b. *-track writes every 3 seconds, in the format `<track-name> - <artists-comma-separated>`
