mod schema;

pub use schema::*;

use crate::error::{Error, Result};
use directories::ProjectDirs;
use std::fs;
use std::path::PathBuf;

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
        let path = Self::config_path()?;

        if !path.exists() {
            return Ok(Self::default());
        }

        let content = fs::read_to_string(&path).map_err(|e| {
            Error::config(format!(
                "Failed to read config file {}: {}",
                path.display(),
                e
            ))
        })?;

        serde_json::from_str(&content).map_err(|e| {
            Error::config(format!(
                "Failed to parse config file {}: {}",
                path.display(),
                e
            ))
        })
    }

    pub fn save(&self) -> Result<()> {
        let path = Self::config_path()?;

        if let Some(parent) = path.parent() {
            fs::create_dir_all(parent).map_err(|e| {
                Error::config(format!(
                    "Failed to create config directory {}: {}",
                    parent.display(),
                    e
                ))
            })?;
        }

        let content = serde_json::to_string_pretty(self)?;
        let tmp_path = path.with_extension("json.tmp");

        #[cfg(unix)]
        {
            use std::os::unix::fs::OpenOptionsExt;
            let mut opts = fs::OpenOptions::new();
            opts.write(true).create(true).truncate(true).mode(0o600);
            let mut file = opts.open(&tmp_path).map_err(|e| {
                Error::config(format!(
                    "Failed to create temp config file {}: {}",
                    tmp_path.display(),
                    e
                ))
            })?;
            std::io::Write::write_all(&mut file, content.as_bytes()).map_err(|e| {
                Error::config(format!(
                    "Failed to write temp config file {}: {}",
                    tmp_path.display(),
                    e
                ))
            })?;
            fs::rename(&tmp_path, &path).map_err(|e| {
                Error::config(format!(
                    "Failed to replace config file {}: {}",
                    path.display(),
                    e
                ))
            })?;
        }

        #[cfg(not(unix))]
        {
            fs::write(&tmp_path, &content).map_err(|e| {
                Error::config(format!(
                    "Failed to write temp config file {}: {}",
                    tmp_path.display(),
                    e
                ))
            })?;
            let _ = fs::remove_file(&path);
            fs::rename(&tmp_path, &path).map_err(|e| {
                Error::config(format!(
                    "Failed to replace config file {}: {}",
                    path.display(),
                    e
                ))
            })?;
        }

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
