mod schema;

pub use schema::*;

use crate::error::{Error, Result};
use directories::ProjectDirs;
use std::fs::{self, File, OpenOptions};
use std::io::Write;
use std::path::{Path, PathBuf};

const CONFIG_FILE: &str = "logchef.json";
const APP_QUALIFIER: &str = "app";
const APP_ORG: &str = "logchef";
const APP_NAME: &str = "logchef";

impl Config {
    pub fn config_dir() -> Result<PathBuf> {
        ProjectDirs::from(APP_QUALIFIER, APP_ORG, APP_NAME)
            .map(|dirs| dirs.config_dir().to_path_buf())
            .ok_or_else(|| Error::config("Could not determine config directory"))
    }

    pub fn config_path() -> Result<PathBuf> {
        Ok(Self::config_dir()?.join(CONFIG_FILE))
    }

    pub fn load() -> Result<Self> {
        Self::load_from(&Self::config_path()?)
    }

    pub fn load_from(path: &Path) -> Result<Self> {
        if !path.exists() {
            return Ok(Self::default());
        }

        let content = fs::read_to_string(path).map_err(|e| {
            Error::config(format!(
                "Failed to read config file {}: {}",
                path.display(),
                e
            ))
        })?;

        let config: Config = serde_json::from_str(&content).map_err(|e| {
            Error::config(format!(
                "Failed to parse config file {}: {}",
                path.display(),
                e
            ))
        })?;

        if config.version > CONFIG_VERSION {
            return Err(Error::config(format!(
                "Config file version {} is newer than supported version {}. Please upgrade logchef CLI.",
                config.version, CONFIG_VERSION
            )));
        }

        Ok(config)
    }

    /// Reads the config, applies `change`, and saves the result while holding
    /// the config-wide lock, so concurrent CLI runs do not lose each other's
    /// updates. Nothing is saved when `change` fails.
    pub fn update<T>(change: impl FnOnce(&mut Config) -> Result<T>) -> Result<T> {
        Self::update_at(&Self::config_path()?, change)
    }

    pub fn update_at<T>(path: &Path, change: impl FnOnce(&mut Config) -> Result<T>) -> Result<T> {
        let dir = parent_dir(path)?;
        ensure_private_dir(dir)?;
        let _lock = lock_exclusive(&path.with_extension("json.lock"))?;
        let mut config = Self::load_from(path)?;
        let value = change(&mut config)?;
        config.write_atomic(path)?;
        Ok(value)
    }

    /// Writes a unique temp file (mode 0600), syncs it, renames it over the
    /// config, then syncs the directory so the rename survives a crash.
    fn write_atomic(&self, path: &Path) -> Result<()> {
        let dir = parent_dir(path)?;
        let content = serde_json::to_string_pretty(self)?;
        let tmp_path = dir.join(format!(
            "{}.{}.{}.tmp",
            CONFIG_FILE,
            std::process::id(),
            random_hex()?
        ));

        let written = (|| -> std::io::Result<()> {
            let mut file = private_options().create_new(true).open(&tmp_path)?;
            file.write_all(content.as_bytes())?;
            file.sync_all()?;
            fs::rename(&tmp_path, path)
        })();
        if let Err(e) = written {
            let _ = fs::remove_file(&tmp_path);
            return Err(Error::config(format!(
                "Failed to save config file {}: {}",
                path.display(),
                e
            )));
        }

        #[cfg(unix)]
        File::open(dir)
            .and_then(|d| d.sync_all())
            .map_err(|e| Error::config(format!("Failed to sync {}: {}", dir.display(), e)))?;

        Ok(())
    }

    pub fn current_context_name(&self) -> Option<&str> {
        self.current_context.as_deref()
    }

    pub fn current_context(&self) -> Option<&Context> {
        self.current_context
            .as_ref()
            .and_then(|name| self.contexts.get(name))
    }

    pub fn current_context_mut(&mut self) -> Option<&mut Context> {
        let name = self.current_context.clone()?;
        self.contexts.get_mut(&name)
    }

    pub fn get_context(&self, name: &str) -> Option<&Context> {
        self.contexts.get(name)
    }

    pub fn get_context_mut(&mut self, name: &str) -> Option<&mut Context> {
        self.contexts.get_mut(name)
    }

    pub fn find_context_by_url(&self, url: &str) -> Option<(&str, &Context)> {
        self.contexts
            .iter()
            .find(|(_, ctx)| ctx.server_url == url)
            .map(|(name, ctx)| (name.as_str(), ctx))
    }

    pub fn use_context(&mut self, name: &str) -> Result<()> {
        if !self.contexts.contains_key(name) {
            return Err(Error::config(format!("Context '{}' not found", name)));
        }
        self.current_context = Some(name.to_string());
        Ok(())
    }

    pub fn add_context(&mut self, name: String, context: Context) -> Result<()> {
        if self.contexts.contains_key(&name) {
            return Err(Error::config(format!("Context '{}' already exists", name)));
        }
        self.contexts.insert(name.clone(), context);
        if self.current_context.is_none() {
            self.current_context = Some(name);
        }
        Ok(())
    }

    pub fn add_or_update_context(&mut self, name: String, context: Context) {
        self.contexts.insert(name.clone(), context);
        self.current_context = Some(name);
    }

    pub fn delete_context(&mut self, name: &str) -> Result<()> {
        if !self.contexts.contains_key(name) {
            return Err(Error::config(format!("Context '{}' not found", name)));
        }
        self.contexts.remove(name);
        if self.current_context.as_deref() == Some(name) {
            self.current_context = self.contexts.keys().next().cloned();
        }
        Ok(())
    }

    pub fn rename_context(&mut self, old_name: &str, new_name: &str) -> Result<()> {
        if !self.contexts.contains_key(old_name) {
            return Err(Error::config(format!("Context '{}' not found", old_name)));
        }
        if self.contexts.contains_key(new_name) {
            return Err(Error::config(format!(
                "Context '{}' already exists",
                new_name
            )));
        }
        if let Some(context) = self.contexts.remove(old_name) {
            self.contexts.insert(new_name.to_string(), context);
            if self.current_context.as_deref() == Some(old_name) {
                self.current_context = Some(new_name.to_string());
            }
        }
        Ok(())
    }

    pub fn context_names(&self) -> Vec<&str> {
        self.contexts.keys().map(|s| s.as_str()).collect()
    }

    pub fn is_empty(&self) -> bool {
        self.contexts.is_empty()
    }
}

/// The lock file that serializes refreshes of one context's OAuth grant.
/// The context name is hex-encoded, so any name maps to a safe, unique file.
pub fn context_lock_path(config_path: &Path, context: &str) -> Result<PathBuf> {
    let name: String = context.bytes().map(|b| format!("{b:02x}")).collect();
    Ok(parent_dir(config_path)?
        .join("locks")
        .join(format!("{name}.lock")))
}

/// Blocks until this process holds an exclusive lock on `path`. The lock is
/// released when the returned file is dropped, including on process exit.
pub fn lock_exclusive(path: &Path) -> Result<File> {
    ensure_private_dir(parent_dir(path)?)?;
    let file = private_options()
        .create(true)
        .truncate(false)
        .write(true)
        .open(path)
        .map_err(|e| Error::config(format!("Failed to open lock {}: {}", path.display(), e)))?;
    file.lock()
        .map_err(|e| Error::config(format!("Failed to lock {}: {}", path.display(), e)))?;
    Ok(file)
}

fn parent_dir(path: &Path) -> Result<&Path> {
    path.parent()
        .ok_or_else(|| Error::config(format!("{} has no parent directory", path.display())))
}

/// Creates `dir` (mode 0700) and tightens it to 0700 if it already exists.
/// Credentials live inside, so no other user may list or traverse it.
fn ensure_private_dir(dir: &Path) -> Result<()> {
    let mut builder = fs::DirBuilder::new();
    builder.recursive(true);
    #[cfg(unix)]
    {
        use std::os::unix::fs::{DirBuilderExt, PermissionsExt};
        builder.mode(0o700);
        builder.create(dir).map_err(|e| dir_error(dir, e))?;
        fs::set_permissions(dir, fs::Permissions::from_mode(0o700))
            .map_err(|e| dir_error(dir, e))?;
    }
    #[cfg(not(unix))]
    builder.create(dir).map_err(|e| dir_error(dir, e))?;
    Ok(())
}

fn dir_error(dir: &Path, e: std::io::Error) -> Error {
    Error::config(format!(
        "Failed to create config directory {}: {}",
        dir.display(),
        e
    ))
}

fn private_options() -> OpenOptions {
    let mut opts = OpenOptions::new();
    opts.write(true);
    #[cfg(unix)]
    {
        use std::os::unix::fs::OpenOptionsExt;
        opts.mode(0o600);
    }
    opts
}

fn random_hex() -> Result<String> {
    let mut bytes = [0u8; 8];
    getrandom::fill(&mut bytes)
        .map_err(|e| Error::other(format!("Failed to generate random bytes: {e}")))?;
    Ok(bytes.iter().map(|b| format!("{b:02x}")).collect())
}

#[cfg(test)]
mod tests {
    use super::*;

    /// A fresh directory under the system temp dir, removed on drop.
    struct TempDir(PathBuf);

    impl TempDir {
        fn new() -> Self {
            let dir = std::env::temp_dir().join(format!(
                "logchef-config-test-{}-{}",
                std::process::id(),
                random_hex().unwrap()
            ));
            fs::create_dir_all(&dir).unwrap();
            Self(dir)
        }
    }

    impl Drop for TempDir {
        fn drop(&mut self) {
            let _ = fs::remove_dir_all(&self.0);
        }
    }

    #[test]
    fn concurrent_updates_lose_no_context() {
        let tmp = TempDir::new();
        let path = tmp.0.join("logchef").join(CONFIG_FILE);
        let writers = 24;
        std::thread::scope(|scope| {
            for i in 0..writers {
                let path = &path;
                scope.spawn(move || {
                    Config::update_at(path, |config| {
                        // Widen the read-modify-write window.
                        std::thread::sleep(std::time::Duration::from_millis(2));
                        config.add_or_update_context(
                            format!("ctx-{i}"),
                            Context::new(format!("https://logs-{i}.example.com")),
                        );
                        Ok(())
                    })
                    .unwrap();
                });
            }
        });

        let config = Config::load_from(&path).unwrap();
        let mut names = config.context_names();
        names.sort();
        assert_eq!(names.len(), writers, "lost contexts: {names:?}");

        let leftovers: Vec<_> = fs::read_dir(path.parent().unwrap())
            .unwrap()
            .map(|e| e.unwrap().file_name().to_string_lossy().into_owned())
            .filter(|name| name.ends_with(".tmp"))
            .collect();
        assert!(leftovers.is_empty(), "temp files left: {leftovers:?}");
    }

    #[test]
    fn failed_change_saves_nothing() {
        let tmp = TempDir::new();
        let path = tmp.0.join(CONFIG_FILE);
        Config::update_at(&path, |config| {
            config.add_or_update_context("a".into(), Context::new("https://a.example".into()));
            Ok(())
        })
        .unwrap();
        let err = Config::update_at(&path, |config| {
            config.add_or_update_context("b".into(), Context::new("https://b.example".into()));
            Err::<(), _>(Error::other("boom"))
        });
        assert!(err.is_err());
        assert_eq!(Config::load_from(&path).unwrap().context_names(), ["a"]);
    }

    #[cfg(unix)]
    #[test]
    fn saved_file_is_0600_and_directories_are_0700() {
        use std::os::unix::fs::PermissionsExt;

        let mode = |p: &Path| fs::metadata(p).unwrap().permissions().mode() & 0o777;
        let tmp = TempDir::new();

        // A new directory is created private.
        let dir = tmp.0.join("new").join("logchef");
        let path = dir.join(CONFIG_FILE);
        Config::update_at(&path, |_| Ok(())).unwrap();
        assert_eq!(mode(&path), 0o600);
        assert_eq!(mode(&dir), 0o700);
        assert_eq!(mode(&path.with_extension("json.lock")), 0o600);

        // An existing directory and file from an older CLI are tightened.
        let old_dir = tmp.0.join("old");
        fs::create_dir_all(&old_dir).unwrap();
        fs::set_permissions(&old_dir, fs::Permissions::from_mode(0o755)).unwrap();
        let old_path = old_dir.join(CONFIG_FILE);
        fs::write(&old_path, "{}").unwrap();
        fs::set_permissions(&old_path, fs::Permissions::from_mode(0o644)).unwrap();
        Config::update_at(&old_path, |_| Ok(())).unwrap();
        assert_eq!(mode(&old_path), 0o600);
        assert_eq!(mode(&old_dir), 0o700);

        // The per-context lock lives in a private directory too.
        let lock = context_lock_path(&path, "prod/../x").unwrap();
        let _held = lock_exclusive(&lock).unwrap();
        assert_eq!(lock.parent().unwrap(), dir.join("locks"));
        assert_eq!(lock.file_name().unwrap(), "70726f642f2e2e2f78.lock");
        assert_eq!(mode(lock.parent().unwrap()), 0o700);
        assert_eq!(mode(&lock), 0o600);
    }

    #[test]
    fn context_lock_excludes_a_second_holder() {
        let tmp = TempDir::new();
        let lock = context_lock_path(&tmp.0.join(CONFIG_FILE), "prod").unwrap();
        let held = lock_exclusive(&lock).unwrap();
        let other = OpenOptions::new().write(true).open(&lock).unwrap();
        assert!(other.try_lock().is_err(), "a second open file got the lock");
        drop(held);
        other
            .try_lock()
            .expect("lock is free after the holder drops it");
    }
}
