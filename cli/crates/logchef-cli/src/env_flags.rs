//! Shared env kill-switches for human chrome (banner + update notifier).
//! Everything here is intentionally cheap and side-effect free so it can gate
//! output without ever slowing a command down.

/// True when `name` is set to a non-empty value. Used for the force-OFF
/// switches (`LOGCHEF_NO_BANNER`, `LOGCHEF_NO_UPDATE_CHECK`, `CI`): any
/// non-empty value disables the affordance.
pub fn env_off(name: &str) -> bool {
    std::env::var_os(name).is_some_and(|v| !v.is_empty())
}

/// True in CI environments (`CI` set to a non-empty value). Disables both the
/// banner and the update notifier.
pub fn ci() -> bool {
    env_off("CI")
}

/// Honors the `NO_COLOR` convention: color is disabled when `NO_COLOR` is set
/// to a non-empty value, enabled otherwise.
pub fn color_enabled() -> bool {
    std::env::var_os("NO_COLOR").is_none_or(|v| v.is_empty())
}
