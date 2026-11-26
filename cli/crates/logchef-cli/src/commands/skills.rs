use anyhow::Result;
use clap::{Args, Subcommand};
use include_dir::{Dir, include_dir};
use serde::Serialize;

/// A skill the CLI bundles. Each is embedded at compile time so its content
/// always matches the installed CLI version. The files live INSIDE the crate
/// rather than being pulled from the repo-root `.agents/skills`: an embed path
/// that escapes the Cargo workspace breaks `cross` release builds, which mount
/// only the workspace. `scripts/sync-skill.sh`, CI and the test below keep the
/// copies in sync.
struct Skill {
    name: &'static str,
    /// One-line description surfaced by `skills list`.
    description: &'static str,
    dir: &'static Dir<'static>,
}

static CORE_DIR: Dir<'_> = include_dir!("$CARGO_MANIFEST_DIR/skill");
static SETUP_DIR: Dir<'_> = include_dir!("$CARGO_MANIFEST_DIR/skill-setup");

static SKILLS: [Skill; 2] = [
    Skill {
        name: "core",
        description: "Logchef CLI usage, LogchefQL/SQL/LogsQL query syntax, and log investigation workflows",
        dir: &CORE_DIR,
    },
    Skill {
        name: "setup",
        description: "Set up saved queries, views, alert rules, silences and reports for an app from its repository, previewed and confirmed before any write",
        dir: &SETUP_DIR,
    },
];

#[derive(Args)]
#[command(after_help = "EXAMPLES:
  # List the bundled skills
  logchef skills list

  # Print the core usage guide (LogchefQL/SQL/LogsQL, workflows)
  logchef skills get core

  # Print the guide for setting up Logchef for an app from its repository
  logchef skills get setup

  # Include every reference file (full syntax reference)
  logchef skills get core --full

  # Feed the guide to an agent as JSON
  logchef skills get core --json")]
pub struct SkillsArgs {
    #[command(subcommand)]
    command: Option<SkillsCommand>,

    /// Output machine-readable JSON.
    #[arg(long, global = true)]
    json: bool,
}

#[derive(Subcommand)]
enum SkillsCommand {
    /// List available skills.
    List,

    /// Print a skill's content.
    Get {
        /// Skill name: `core` or `setup`.
        name: String,

        /// Append every reference file under `references/`.
        #[arg(long)]
        full: bool,
    },
}

#[derive(Serialize)]
struct SkillListEntry {
    name: &'static str,
    description: &'static str,
}

#[derive(Serialize)]
struct SkillGetOutput {
    name: String,
    content: String,
    references: std::collections::BTreeMap<String, String>,
}

pub async fn run(args: SkillsArgs) -> Result<()> {
    match args.command {
        None | Some(SkillsCommand::List) => list(args.json),
        Some(SkillsCommand::Get { name, full }) => get(&name, full, args.json),
    }
}

fn list(json: bool) -> Result<()> {
    let entries: Vec<SkillListEntry> = SKILLS
        .iter()
        .map(|skill| SkillListEntry {
            name: skill.name,
            description: skill.description,
        })
        .collect();

    if json {
        println!("{}", serde_json::to_string_pretty(&entries)?);
    } else {
        for entry in &entries {
            println!("{}\t{}", entry.name, entry.description);
        }
    }

    Ok(())
}

fn get(name: &str, full: bool, json: bool) -> Result<()> {
    let Some(skill) = SKILLS.iter().find(|skill| skill.name == name) else {
        let names: Vec<&str> = SKILLS.iter().map(|skill| skill.name).collect();
        anyhow::bail!(
            "Unknown skill '{}'. Valid skills: {}",
            name,
            names.join(", ")
        );
    };

    let content = skill
        .dir
        .get_file("SKILL.md")
        .and_then(|file| file.contents_utf8())
        .ok_or_else(|| anyhow::anyhow!("Embedded skill '{name}' is missing SKILL.md"))?;

    if json {
        let output = SkillGetOutput {
            name: name.to_string(),
            content: content.to_string(),
            references: reference_files(skill.dir).collect(),
        };
        println!("{}", serde_json::to_string_pretty(&output)?);
        return Ok(());
    }

    print!("{}", content);
    if !content.ends_with('\n') {
        println!();
    }

    if full {
        for (path, body) in reference_files(skill.dir) {
            println!("\n---\n# references/{}\n", path);
            print!("{}", body);
            if !body.ends_with('\n') {
                println!();
            }
        }
    }

    Ok(())
}

/// Returns a skill's embedded reference files as `(file_name, content)`
/// pairs, sorted by name for deterministic output.
fn reference_files(dir: &'static Dir<'static>) -> impl Iterator<Item = (String, String)> {
    let mut files: Vec<(String, String)> = dir
        .get_dir("references")
        .into_iter()
        .flat_map(|dir| dir.files())
        .filter_map(|file| {
            let name = file.path().file_name()?.to_string_lossy().into_owned();
            let body = file.contents_utf8()?.to_string();
            Some((name, body))
        })
        .collect();
    files.sort_by(|a, b| a.0.cmp(&b.0));
    files.into_iter()
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::collections::BTreeMap;
    use std::path::{Path, PathBuf};

    #[test]
    fn lists_the_core_and_setup_skills() {
        let names: Vec<&str> = SKILLS.iter().map(|skill| skill.name).collect();
        assert_eq!(names, ["core", "setup"]);
        for skill in &SKILLS {
            let content = skill
                .dir
                .get_file("SKILL.md")
                .and_then(|file| file.contents_utf8())
                .unwrap_or_else(|| panic!("{} has no SKILL.md", skill.name));
            assert!(
                content.starts_with("---\nname: "),
                "{} has no frontmatter",
                skill.name
            );
        }
    }

    /// Every embedded skill equals its canonical copy in `.agents/skills`,
    /// file for file. Run `cli/scripts/sync-skill.sh` after editing one.
    #[test]
    fn embedded_skills_match_their_canonical_copies() {
        let canonical_root = Path::new(env!("CARGO_MANIFEST_DIR")).join("../../../.agents/skills");
        for (skill, canonical) in [("core", "logchef"), ("setup", "logchef-setup")] {
            let embedded = SKILLS.iter().find(|s| s.name == skill).unwrap();
            let mut embedded_files = BTreeMap::new();
            collect_embedded(embedded.dir, &mut embedded_files);
            let mut canonical_files = BTreeMap::new();
            collect_disk(
                &canonical_root.join(canonical),
                Path::new(""),
                &mut canonical_files,
            );
            assert_eq!(
                embedded_files, canonical_files,
                "the embedded {skill} skill differs from .agents/skills/{canonical}: run cli/scripts/sync-skill.sh"
            );
        }
    }

    fn collect_embedded(dir: &Dir<'_>, out: &mut BTreeMap<PathBuf, Vec<u8>>) {
        for file in dir.files() {
            out.insert(file.path().to_path_buf(), file.contents().to_vec());
        }
        for child in dir.dirs() {
            collect_embedded(child, out);
        }
    }

    fn collect_disk(root: &Path, rel: &Path, out: &mut BTreeMap<PathBuf, Vec<u8>>) {
        for entry in std::fs::read_dir(root.join(rel)).unwrap() {
            let entry = entry.unwrap();
            let path = rel.join(entry.file_name());
            if entry.file_type().unwrap().is_dir() {
                collect_disk(root, &path, out);
            } else {
                out.insert(path.clone(), std::fs::read(root.join(&path)).unwrap());
            }
        }
    }
}
