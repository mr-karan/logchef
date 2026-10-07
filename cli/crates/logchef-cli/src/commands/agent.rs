use anyhow::{Context, Result, bail};
use clap::{Args, Subcommand, ValueEnum};
use logchef_core::Config;
use logchef_core::config::ContextAuth;
use serde::Serialize;
use url::Url;

use super::doctor::{self, Check};
use crate::cli::GlobalArgs;
use crate::session;

const URL_PLACEHOLDER: &str = "<your-logchef-url>";

#[derive(Args)]
pub struct AgentArgs {
    #[command(subcommand)]
    command: AgentCommand,
}

#[derive(Subcommand)]
enum AgentCommand {
    /// Preview the steps to use Logchef from a coding agent. Changes nothing.
    #[command(after_help = "EXAMPLES:
  # Human-readable preview with doctor checks
  logchef agent setup

  # Structured output for an agent or script
  logchef agent setup --host claude-code --output json | jq '.steps[].command'")]
    Setup {
        /// Agent host to prepare for. Default: all hosts.
        #[arg(long, value_enum)]
        host: Option<Host>,

        /// Output format.
        #[arg(long, default_value = "text")]
        output: OutputFormat,
    },
}

#[derive(Clone, Copy, Debug, PartialEq, ValueEnum)]
enum Host {
    ClaudeCode,
    Codex,
    Cursor,
    Chatgpt,
}

#[derive(Clone, Copy, Debug, ValueEnum)]
enum OutputFormat {
    Text,
    Json,
    Jsonl,
}

#[derive(Serialize)]
struct Preview {
    mode: &'static str,
    mutates: bool,
    instance: Instance,
    checks: Vec<Check>,
    steps: Vec<Step>,
}

#[derive(Serialize)]
struct Instance {
    context: Option<String>,
    server_url: Option<String>,
}

#[derive(Serialize)]
struct Step {
    id: &'static str,
    description: &'static str,
    command: String,
}

pub async fn run(args: AgentArgs, global: GlobalArgs) -> Result<()> {
    let AgentCommand::Setup { host, output } = args.command;

    let resolved = resolve_instance(&global)?;
    let doctor::Diagnosis {
        checks,
        oauth_enabled,
    } = doctor::collect_checks(&global).await;
    let steps = if host == Some(Host::Chatgpt) {
        Vec::new()
    } else {
        let plan = authentication(resolved.token_resolved, oauth_enabled);
        if plan == Authentication::OAuthUnverified && !global.quiet {
            eprintln!(
                "Note: could not read server metadata. The auth step applies only to instances with Logchef OAuth."
            );
        }
        steps(&resolved.instance, plan)
    };

    if !global.quiet && matches!(host, None | Some(Host::Chatgpt)) {
        eprintln!(
            "Note: ChatGPT connects through the remote app. This command does not configure it."
        );
    }

    let preview = Preview {
        mode: "preview",
        mutates: false,
        instance: resolved.instance,
        checks,
        steps,
    };

    match output {
        OutputFormat::Json => println!("{}", serde_json::to_string_pretty(&preview)?),
        OutputFormat::Jsonl => println!("{}", serde_json::to_string(&preview)?),
        OutputFormat::Text => print_text(&preview),
    }
    Ok(())
}

struct ResolvedInstance {
    instance: Instance,
    token_resolved: bool,
}

/// How the person gets a usable token for the selected instance.
#[derive(Clone, Copy, Debug, PartialEq)]
enum Authentication {
    /// A usable credential already resolves (flag, env, unexpired saved
    /// token, or a saved OAuth grant).
    NotNeeded,
    /// The server offers Logchef OAuth sign-in.
    OAuth,
    /// Server metadata was unavailable. Logchef OAuth is assumed.
    OAuthUnverified,
    /// The server has no Logchef OAuth. Use an API token.
    ApiToken,
}

fn authentication(token_resolved: bool, oauth_enabled: Option<bool>) -> Authentication {
    match (token_resolved, oauth_enabled) {
        (true, _) => Authentication::NotNeeded,
        (false, Some(true)) => Authentication::OAuth,
        (false, Some(false)) => Authentication::ApiToken,
        (false, None) => Authentication::OAuthUnverified,
    }
}

/// Resolves the instance the way every other command does. An explicit
/// `--context` that does not exist is an error and never falls back to
/// `--server`. With nothing configured, the preview continues with no instance.
fn resolve_instance(global: &GlobalArgs) -> Result<ResolvedInstance> {
    let config = Config::load().ok();
    let resolved = match &config {
        Some(config) => match session::resolve(config, global) {
            Ok(resolved) => Some(resolved),
            Err(err) if global.context.is_some() => return Err(err),
            Err(_) => None,
        },
        None => match &global.context {
            Some(name) => bail!("Context '{name}' not found"),
            None => None,
        },
    };

    let Some(resolved) = resolved else {
        let server_url = global
            .server
            .as_deref()
            .map(sanitize_server_url)
            .transpose()
            .context("Invalid server URL")?;
        return Ok(ResolvedInstance {
            instance: Instance {
                context: None,
                server_url,
            },
            token_resolved: global.token.is_some(),
        });
    };

    let server_url = sanitize_server_url(&resolved.ctx.server_url).context("Invalid server URL")?;
    // An OAuth grant renews itself, so only a PAT can be unusable by expiry.
    let saved_token_usable = match &resolved.ctx.auth {
        Some(ContextAuth::Pat { expires_at, .. }) => {
            expires_at.is_none_or(|expiry| expiry > chrono::Utc::now())
        }
        Some(ContextAuth::OAuth(_)) => true,
        None => false,
    };
    Ok(ResolvedInstance {
        instance: Instance {
            context: (!resolved.is_ephemeral).then_some(resolved.name),
            server_url: Some(server_url),
        },
        token_resolved: global.token.is_some() || saved_token_usable,
    })
}

/// Accepts only a plain http(s) URL. Userinfo, query, and fragment can carry
/// credentials, so they are rejected rather than echoed into commands or JSON.
/// Error text never includes the rejected URL.
fn sanitize_server_url(raw: &str) -> Result<String> {
    let url = Url::parse(raw).map_err(|_| anyhow::anyhow!("not a valid URL"))?;
    if !matches!(url.scheme(), "http" | "https") {
        bail!("scheme must be http or https");
    }
    if !url.username().is_empty() || url.password().is_some() {
        bail!("must not contain a username or password");
    }
    if url.query().is_some() || url.fragment().is_some() {
        bail!("must not contain a query or fragment");
    }
    Ok(url.as_str().trim_end_matches('/').to_string())
}

/// Single-quotes a value for POSIX shells. An embedded `'` becomes `'\\''`.
fn shell_quote(value: &str) -> String {
    format!("'{}'", value.replace('\'', "'\\''"))
}

fn steps(instance: &Instance, authentication: Authentication) -> Vec<Step> {
    let mut steps = Vec::new();
    match authentication {
        Authentication::NotNeeded => {}
        Authentication::OAuth | Authentication::OAuthUnverified => steps.push(Step {
            id: "auth",
            description: "Sign in with Logchef OAuth in a browser and save the grant for this instance",
            command: auth_command(instance),
        }),
        Authentication::ApiToken => steps.push(Step {
            id: "token",
            description: "This server does not offer Logchef OAuth sign-in. Upgrade it, or create an API token in the web UI and set it",
            command: "export LOGCHEF_AUTH_TOKEN=<your-api-token>".to_string(),
        }),
    }
    steps.push(Step {
        id: "verify",
        description: "Check config, connectivity, token, and defaults",
        command: doctor_command(instance),
    });
    steps.push(Step {
        id: "skills",
        description: "Read the bundled usage guide for LogchefQL, SQL, and LogsQL",
        command: "logchef skills get core".to_string(),
    });
    steps
}

/// Targets the same instance as the auth step, so a token-only setup with
/// `--server` and no saved context still checks the selected server.
fn doctor_command(instance: &Instance) -> String {
    match (&instance.context, &instance.server_url) {
        (Some(context), _) => format!("logchef doctor --json --context {}", shell_quote(context)),
        (None, Some(url)) => format!("logchef doctor --json --server {}", shell_quote(url)),
        (None, None) => "logchef doctor --json".to_string(),
    }
}

/// A named context already stores its server, so the command targets the
/// context. Otherwise it targets the server URL, or the placeholder.
fn auth_command(instance: &Instance) -> String {
    match (&instance.context, &instance.server_url) {
        (Some(context), _) => format!("logchef auth --context {}", shell_quote(context)),
        (None, Some(url)) => format!("logchef auth --server {}", shell_quote(url)),
        (None, None) => format!("logchef auth --server {URL_PLACEHOLDER}"),
    }
}

fn print_text(preview: &Preview) {
    println!("Preview only. Nothing was changed.\n");
    println!(
        "Instance: {} (context: {})\n",
        preview
            .instance
            .server_url
            .as_deref()
            .unwrap_or(URL_PLACEHOLDER),
        preview.instance.context.as_deref().unwrap_or("none"),
    );
    println!("Checks:");
    for check in &preview.checks {
        println!(
            "  {} {}  {}",
            check.status.glyph(),
            check.check,
            check.detail
        );
        if let Some(hint) = &check.hint {
            println!("      → {hint}");
        }
    }
    if preview.steps.is_empty() {
        return;
    }
    println!("\nSteps:");
    for (i, step) in preview.steps.iter().enumerate() {
        println!("  {}. {}\n     {}", i + 1, step.description, step.command);
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn sanitize_accepts_plain_urls_and_trims_slash() {
        assert_eq!(
            sanitize_server_url("https://logs.example.com/").unwrap(),
            "https://logs.example.com"
        );
        assert_eq!(
            sanitize_server_url("http://localhost:8125").unwrap(),
            "http://localhost:8125"
        );
    }

    #[test]
    fn sanitize_rejects_credentials_query_fragment_and_bad_scheme() {
        for bad in [
            "https://user:pw@logs.example.com",
            "https://user@logs.example.com",
            "https://logs.example.com/?token=abc",
            "https://logs.example.com/#frag",
            "ftp://logs.example.com",
            "not a url",
        ] {
            let err = sanitize_server_url(bad).unwrap_err().to_string();
            assert!(!err.contains("pw") && !err.contains("abc"), "leaked: {err}");
        }
    }

    fn instance(context: Option<&str>, url: Option<&str>) -> Instance {
        Instance {
            context: context.map(str::to_string),
            server_url: url.map(str::to_string),
        }
    }

    fn auth_step(instance: &Instance) -> String {
        steps(instance, Authentication::OAuth)[0].command.clone()
    }

    #[test]
    fn auth_step_uses_placeholder_without_server() {
        assert_eq!(
            auth_step(&instance(None, None)),
            "logchef auth --server <your-logchef-url>"
        );
    }

    #[test]
    fn auth_step_quotes_shell_metacharacters_in_url() {
        for (url, quoted) in [
            ("https://h.example.com/;id", "'https://h.example.com/;id'"),
            (
                "https://h.example.com/$(id)",
                "'https://h.example.com/$(id)'",
            ),
            (
                "https://h.example.com/a'b",
                "'https://h.example.com/a'\\''b'",
            ),
            ("https://h.example.com/a&b", "'https://h.example.com/a&b'"),
            ("https://h.example.com/a b", "'https://h.example.com/a%20b'"),
            (
                "https://h.example.com/a%3Bb",
                "'https://h.example.com/a%3Bb'",
            ),
        ] {
            let url = sanitize_server_url(url).unwrap();
            assert_eq!(
                auth_step(&instance(None, Some(&url))),
                format!("logchef auth --server {quoted}")
            );
        }
    }

    #[test]
    fn named_context_targets_the_context_with_quoting() {
        assert_eq!(
            auth_step(&instance(Some("prod"), Some("https://logs.example.com"))),
            "logchef auth --context 'prod'"
        );
        assert_eq!(
            auth_step(&instance(Some("a;b'c d"), Some("https://logs.example.com"))),
            "logchef auth --context 'a;b'\\''c d'"
        );
    }

    #[test]
    fn two_contexts_sharing_a_url_keep_their_own_name_in_steps() {
        let url = "https://logs.example.com";
        let second = instance(Some("second"), Some(url));
        let steps = steps(&second, Authentication::OAuth);
        assert_eq!(steps[0].command, "logchef auth --context 'second'");
        assert_eq!(steps[1].command, "logchef doctor --json --context 'second'");
    }

    #[test]
    fn token_only_server_is_verified_against_that_server() {
        let ephemeral = instance(None, Some("https://logs.example.com/a;b"));
        let steps = steps(&ephemeral, authentication(true, Some(true)));
        let ids: Vec<_> = steps.iter().map(|s| s.id).collect();
        assert_eq!(ids, ["verify", "skills"]);
        assert_eq!(
            steps[0].command,
            "logchef doctor --json --server 'https://logs.example.com/a;b'"
        );
    }

    #[test]
    fn token_present_gives_no_auth_step() {
        let steps = steps(&instance(None, None), authentication(true, Some(true)));
        let ids: Vec<_> = steps.iter().map(|s| s.id).collect();
        assert_eq!(ids, ["verify", "skills"]);
    }

    #[test]
    fn oauth_absent_gives_token_env_step_and_no_auth_step() {
        let steps = steps(&instance(None, None), authentication(false, Some(false)));
        let ids: Vec<_> = steps.iter().map(|s| s.id).collect();
        assert_eq!(ids, ["token", "verify", "skills"]);
        assert!(steps[0].command.contains("LOGCHEF_AUTH_TOKEN"));
        assert!(steps.iter().all(|s| !s.command.starts_with("logchef auth")));
    }

    #[test]
    fn unknown_metadata_keeps_auth_step() {
        assert_eq!(authentication(false, None), Authentication::OAuthUnverified);
        let steps = steps(&instance(None, None), Authentication::OAuthUnverified);
        assert_eq!(steps[0].id, "auth");
    }

    #[test]
    fn explicit_missing_context_fails_without_server_fallback() {
        let global = GlobalArgs {
            context: Some("no-such-context-for-test".to_string()),
            server: Some("https://other.example.com".to_string()),
            token: None,
            quiet: true,
        };
        let err = resolve_instance(&global).err().expect("must fail");
        assert!(err.to_string().contains("no-such-context-for-test"));
    }

    #[test]
    fn preview_json_has_agreed_shape() {
        let preview = Preview {
            mode: "preview",
            mutates: false,
            instance: Instance {
                context: None,
                server_url: None,
            },
            checks: Vec::new(),
            steps: steps(&instance(None, None), Authentication::OAuth),
        };
        let value = serde_json::to_value(&preview).unwrap();
        let mut keys: Vec<_> = value.as_object().unwrap().keys().cloned().collect();
        keys.sort();
        assert_eq!(keys, ["checks", "instance", "mode", "mutates", "steps"]);
        assert_eq!(value["mutates"], false);
    }
}
