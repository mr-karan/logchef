//! Startup ASCII banner shown on bare `logchef` (the `None` command branch),
//! printed to stdout right before the help text. It is pure human chrome: it
//! only appears when stdout is a TTY, the `show_banner` config flag is enabled,
//! and no env kill-switch (`LOGCHEF_NO_BANNER`, `CI`) is set. Color is dropped
//! under `NO_COLOR`, but the art still prints.

use std::io::IsTerminal;

// Logchef brand blue (#2563EB = RGB 37,99,235, matching LOGCHEF.svg) truecolor
// + dim, mirroring the raw-ANSI approach already used in `ui.rs` (no color crate
// dependency).
const BRAND: &str = "\x1b[38;2;37;99;235m";
const DIM: &str = "\x1b[2m";
const RESET: &str = "\x1b[0m";

// ansi_shadow figlet of "LOGCHEF". Box-drawing characters and leading spaces
// are load-bearing — preserve exactly.
const ART: &str = r"██╗      ██████╗  ██████╗  ██████╗██╗  ██╗███████╗███████╗
██║     ██╔═══██╗██╔════╝ ██╔════╝██║  ██║██╔════╝██╔════╝
██║     ██║   ██║██║  ███╗██║     ███████║█████╗  █████╗
██║     ██║   ██║██║   ██║██║     ██╔══██║██╔══╝  ██╔══╝
███████╗╚██████╔╝╚██████╔╝╚██████╗██║  ██║███████╗██║
╚══════╝ ╚═════╝  ╚═════╝  ╚═════╝╚═╝  ╚═╝╚══════╝╚═╝";

const TAGLINE: &str = "Search and investigate logs from your terminal";

/// Prints the startup banner to stdout when `show_banner` is enabled, stdout is
/// a TTY, and no env kill-switch is set. No-op otherwise, so piped/`CI` runs
/// stay byte-clean.
pub fn print_banner(show_banner: bool) {
    if !show_banner || crate::env_flags::env_off("LOGCHEF_NO_BANNER") || crate::env_flags::ci() {
        return;
    }
    if !std::io::stdout().is_terminal() {
        return;
    }

    let version = env!("CARGO_PKG_VERSION");
    if crate::env_flags::color_enabled() {
        for line in ART.lines() {
            println!("{BRAND}{line}{RESET}");
        }
        println!("{DIM}  {TAGLINE}   v{version}{RESET}");
    } else {
        println!("{ART}");
        println!("  {TAGLINE}   v{version}");
    }
    println!();
}
