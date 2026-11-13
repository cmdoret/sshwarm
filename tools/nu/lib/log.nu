#!/usr/bin/env nu
use std/log

export-env {
    $env.NU_LOG_FORMAT = ($env.NU_LOG_FORMAT? | default "🐝 %ANSI_START%%LEVEL%%ANSI_STOP% | %MSG%")
}

export def "log error" [message: string] {
    log custom $message $env.NU_LOG_FORMAT (log log-level | get ERROR) --ansi (ansi red_reverse) --level-prefix "ERR"
}

export def "log warning" [message: string] {
    log custom $message $env.NU_LOG_FORMAT (log log-level | get WARNING) --ansi (ansi yellow_reverse) --level-prefix "WRN"
}

export def "log info" [message: string] {
    log custom $message $env.NU_LOG_FORMAT (log log-level | get INFO) --ansi (ansi cyan_reverse) --level-prefix "INF"
}

export def "log debug" [message: string] {
    log custom $message $env.NU_LOG_FORMAT (log log-level | get DEBUG) --ansi (ansi magenta_reverse) --level-prefix "DBG"
}

