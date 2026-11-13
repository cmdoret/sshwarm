#!/usr/bin/env nu

export def spinner [
    message?: string = "Working...",
    interval?: duration = 250ms,
] {
    let frames = ["🍯  🐝"  "🍯🐝  "  "🐝    " "  🐝  " "    🐝"]
    mut index = 0

    loop {
        let frame = ($frames | get ($index mod ($frames | length)))
        print --no-newline $"\r($frame) | ($message)"

        $index += 1
        sleep $interval
    }
}
