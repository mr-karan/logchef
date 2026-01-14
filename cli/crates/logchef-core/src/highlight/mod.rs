use tailspin::config::{
    DateTimeConfig, IpV4Config, IpV6Config, JsonConfig, KeyValueConfig, KeywordConfig,
    NumberConfig, PointerConfig, QuotesConfig, RegexConfig, UnixPathConfig, UnixProcessConfig,
    UrlConfig, UuidConfig,
};
use tailspin::style::{Color, Style};
use tailspin::Highlighter as TailspinHighlighter;

use crate::config::HighlightsConfig;
use crate::error::Result;

pub struct Highlighter {
    inner: TailspinHighlighter,
}

#[derive(Default, Clone)]
pub struct HighlightOptions {
    pub adhoc_highlights: Vec<(String, Vec<String>)>,
    pub disabled_groups: Vec<String>,
}

impl Highlighter {
    pub fn new(config: &HighlightsConfig) -> Result<Self> {
        Self::with_options(config, &HighlightOptions::default())
    }

    pub fn with_options(config: &HighlightsConfig, options: &HighlightOptions) -> Result<Self> {
        let mut builder = TailspinHighlighter::builder();

        let disabled: Vec<&str> = config
            .disabled_groups
            .iter()
            .chain(options.disabled_groups.iter())
            .map(|s| s.as_str())
            .collect();

        let mut keywords = Vec::new();

        if !config.disable_builtin && !disabled.contains(&"keywords") {
            keywords.extend(default_log_level_keywords());
        }

        if !config.custom_keywords.is_empty() {
            keywords.push(KeywordConfig {
                words: config.custom_keywords.clone(),
                style: Style::new().fg(Color::Magenta).bold(),
            });
        }

        for (color, words) in &options.adhoc_highlights {
            let style = parse_color_style(color);
            keywords.push(KeywordConfig {
                words: words.clone(),
                style,
            });
        }

        if !keywords.is_empty() {
            builder.with_keyword_highlighter(keywords);
        }

        for regex_cfg in &config.custom_regexes {
            let style = parse_color_style(&regex_cfg.color)
                .bold_if(regex_cfg.bold)
                .italic_if(regex_cfg.italic);
            builder.with_regex_highlighter(RegexConfig {
                regex: regex_cfg.pattern.clone(),
                style,
            });
        }

        if !disabled.contains(&"dates") {
            builder.with_date_time_highlighters(DateTimeConfig::default());
        }
        if !disabled.contains(&"numbers") {
            builder.with_number_highlighter(NumberConfig::default());
        }
        if !disabled.contains(&"uuids") {
            builder.with_uuid_highlighter(UuidConfig::default());
        }
        if !disabled.contains(&"ips") {
            builder.with_ip_v4_highlighter(IpV4Config::default());
            builder.with_ip_v6_highlighter(IpV6Config::default());
        }
        if !disabled.contains(&"urls") {
            builder.with_url_highlighter(UrlConfig::default());
        }
        if !disabled.contains(&"paths") {
            builder.with_unix_path_highlighter(UnixPathConfig::default());
            builder.with_unix_process_highlighter(UnixProcessConfig::default());
        }
        if !disabled.contains(&"pointers") {
            builder.with_pointer_highlighter(PointerConfig::default());
        }
        if !disabled.contains(&"keyvalue") {
            builder.with_key_value_highlighter(KeyValueConfig::default());
        }
        if !disabled.contains(&"quotes") {
            builder.with_quote_highlighter(QuotesConfig::default());
        }
        if !disabled.contains(&"json") {
            builder.with_json_highlighter(JsonConfig::default());
        }

        let inner = builder
            .build()
            .map_err(|e| crate::error::Error::Config(e.to_string()))?;

        Ok(Self { inner })
    }

    pub fn highlight(&self, line: &str) -> String {
        self.inner.apply(line).to_string()
    }
}

impl Default for Highlighter {
    fn default() -> Self {
        Self::new(&HighlightsConfig::default()).unwrap_or_else(|_| Self {
            inner: TailspinHighlighter::default(),
        })
    }
}

fn parse_color_style(color: &str) -> Style {
    let color = match color.to_lowercase().as_str() {
        "red" => Color::Red,
        "green" => Color::Green,
        "yellow" => Color::Yellow,
        "blue" => Color::Blue,
        "magenta" => Color::Magenta,
        "cyan" => Color::Cyan,
        "white" => Color::White,
        "black" => Color::Black,
        "bright_red" | "brightred" => Color::BrightRed,
        "bright_green" | "brightgreen" => Color::BrightGreen,
        "bright_yellow" | "brightyellow" => Color::BrightYellow,
        "bright_blue" | "brightblue" => Color::BrightBlue,
        "bright_magenta" | "brightmagenta" => Color::BrightMagenta,
        "bright_cyan" | "brightcyan" => Color::BrightCyan,
        "bright_white" | "brightwhite" => Color::BrightWhite,
        _ => Color::Magenta,
    };
    Style::new().fg(color)
}

trait StyleExt {
    fn bold_if(self, cond: bool) -> Self;
    fn italic_if(self, cond: bool) -> Self;
}

impl StyleExt for Style {
    fn bold_if(self, cond: bool) -> Self {
        if cond {
            self.bold()
        } else {
            self
        }
    }

    fn italic_if(self, cond: bool) -> Self {
        if cond {
            self.italic()
        } else {
            self
        }
    }
}

fn default_log_level_keywords() -> Vec<KeywordConfig> {
    vec![
        KeywordConfig {
            words: vec![
                "ERROR".to_string(),
                "FATAL".to_string(),
                "CRITICAL".to_string(),
                "error".to_string(),
                "fatal".to_string(),
                "critical".to_string(),
            ],
            style: Style::new().fg(Color::Red).bold(),
        },
        KeywordConfig {
            words: vec![
                "WARN".to_string(),
                "WARNING".to_string(),
                "warn".to_string(),
                "warning".to_string(),
            ],
            style: Style::new().fg(Color::Yellow),
        },
        KeywordConfig {
            words: vec!["INFO".to_string(), "info".to_string()],
            style: Style::new().fg(Color::Green),
        },
        KeywordConfig {
            words: vec![
                "DEBUG".to_string(),
                "TRACE".to_string(),
                "debug".to_string(),
                "trace".to_string(),
            ],
            style: Style::new().fg(Color::Blue),
        },
        KeywordConfig {
            words: vec!["GET".to_string()],
            style: Style::new().fg(Color::Green).bold(),
        },
        KeywordConfig {
            words: vec!["POST".to_string()],
            style: Style::new().fg(Color::Yellow).bold(),
        },
        KeywordConfig {
            words: vec!["PUT".to_string(), "PATCH".to_string()],
            style: Style::new().fg(Color::Magenta).bold(),
        },
        KeywordConfig {
            words: vec!["DELETE".to_string()],
            style: Style::new().fg(Color::Red).bold(),
        },
        KeywordConfig {
            words: vec!["null".to_string(), "true".to_string(), "false".to_string()],
            style: Style::new().fg(Color::Cyan),
        },
    ]
}

pub fn format_log_entry(entry: &crate::api::LogEntry, columns: &[crate::api::Column]) -> String {
    let priority_fields = ["_timestamp", "timestamp", "level", "severity", "msg", "message"];

    let mut parts = Vec::new();

    for field in priority_fields {
        if let Some(value) = entry.get(field) {
            parts.push(format_value(field, value));
        }
    }

    for col in columns {
        if !priority_fields.contains(&col.name.as_str()) && !col.name.starts_with('_') {
            if let Some(value) = entry.get(&col.name) {
                if !value.is_null() {
                    parts.push(format_value(&col.name, value));
                }
            }
        }
    }

    parts.join(" ")
}

fn format_value(key: &str, value: &serde_json::Value) -> String {
    match value {
        serde_json::Value::String(s) => {
            if key == "_timestamp" || key == "timestamp" {
                s.clone()
            } else if key == "level" || key == "severity" {
                format!("[{}]", s.to_uppercase())
            } else if key == "msg" || key == "message" {
                s.clone()
            } else {
                format!("{}={}", key, s)
            }
        }
        serde_json::Value::Number(n) => {
            if key == "_timestamp" || key == "timestamp" {
                n.to_string()
            } else {
                format!("{}={}", key, n)
            }
        }
        serde_json::Value::Bool(b) => format!("{}={}", key, b),
        serde_json::Value::Null => String::new(),
        serde_json::Value::Array(arr) => format!("{}={:?}", key, arr),
        serde_json::Value::Object(obj) => format!("{}={}", key, serde_json::to_string(obj).unwrap_or_default()),
    }
}
