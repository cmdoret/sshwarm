#!/usr/bin/env nu

use lib/log.nu *


# Log every line of the positional arg, or of stdin if no arg is given
def log-lines [message: any, logger: closure] {
    let source = if $message != null { $message | lines } else { ^cat | lines }
    $source
    | where {|line| $line | str trim | is-not-empty }   # skip blank lines
    | each {|line| do $logger $line }
    | ignore   # drain the stream without printing the (null) results
}

export def "main info" [message?: string] { log-lines $message {|m| log info $m } }
export def "main warn" [message?: string] { log-lines $message {|m| log warning $m } }
export def "main err"  [message?: string] { log-lines $message {|m| log error $m } }

def main [] {}
