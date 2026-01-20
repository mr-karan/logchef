use directories::ProjectDirs;
use serde::{Deserialize, Serialize};
use std::collections::HashMap;
use std::fs;
use std::path::PathBuf;
use std::time::{SystemTime, UNIX_EPOCH};

const CACHE_TTL_SECS: u64 = 600; // 10 minutes

#[derive(Debug, Default, Serialize, Deserialize)]
struct CacheData {
    teams: HashMap<String, TeamCache>,
    #[serde(default)]
    updated_at: u64,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
struct TeamCache {
    id: i64,
    sources: HashMap<String, i64>,
}

pub struct Cache {
    path: PathBuf,
    data: CacheData,
}

impl Cache {
    pub fn new(server_url: &str) -> Self {
        let path = Self::cache_path(server_url);
        let data = Self::load_from_disk(&path).unwrap_or_default();
        Self { path, data }
    }

    fn cache_path(server_url: &str) -> PathBuf {
        let dirs =
            ProjectDirs::from("", "", "logchef").expect("Could not determine project directories");
        let cache_dir = dirs.cache_dir();
        fs::create_dir_all(cache_dir).ok();

        let safe_name: String = server_url.replace("://", "_").replace(['/', ':', '.'], "_");
        cache_dir.join(format!("resolve_{}.json", safe_name))
    }

    fn load_from_disk(path: &PathBuf) -> Option<CacheData> {
        let content = fs::read_to_string(path).ok()?;
        serde_json::from_str(&content).ok()
    }

    fn save_to_disk(&self) {
        if let Ok(content) = serde_json::to_string_pretty(&self.data) {
            fs::write(&self.path, content).ok();
        }
    }

    fn is_expired(&self) -> bool {
        let now = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .unwrap_or_default()
            .as_secs();
        now.saturating_sub(self.data.updated_at) > CACHE_TTL_SECS
    }

    fn touch(&mut self) {
        self.data.updated_at = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .unwrap_or_default()
            .as_secs();
    }

    pub fn get_team_id(&self, name: &str) -> Option<i64> {
        if self.is_expired() {
            return None;
        }
        self.data.teams.get(name).map(|t| t.id)
    }

    pub fn get_source_id(&self, team_id: i64, source_name: &str) -> Option<i64> {
        if self.is_expired() {
            return None;
        }
        self.data
            .teams
            .values()
            .find(|t| t.id == team_id)
            .and_then(|t| t.sources.get(source_name).copied())
    }

    pub fn set_teams(&mut self, teams: &[(String, i64)]) {
        for (name, id) in teams {
            self.data
                .teams
                .entry(name.clone())
                .and_modify(|t| t.id = *id)
                .or_insert_with(|| TeamCache {
                    id: *id,
                    sources: HashMap::new(),
                });
        }
        self.touch();
        self.save_to_disk();
    }

    pub fn set_sources(&mut self, team_id: i64, sources: &[(String, i64)]) {
        if let Some(team) = self.data.teams.values_mut().find(|t| t.id == team_id) {
            for (name, id) in sources {
                team.sources.insert(name.clone(), *id);
            }
            self.touch();
            self.save_to_disk();
        }
    }

    pub fn clear(&mut self) {
        self.data = CacheData::default();
        fs::remove_file(&self.path).ok();
    }
}

pub fn parse_identifier(input: &str) -> Identifier {
    if input.chars().all(|c| c.is_ascii_digit())
        && let Ok(id) = input.parse::<i64>()
    {
        return Identifier::Id(id);
    }
    Identifier::Name(input.to_string())
}

#[derive(Debug, Clone)]
pub enum Identifier {
    Id(i64),
    Name(String),
}
