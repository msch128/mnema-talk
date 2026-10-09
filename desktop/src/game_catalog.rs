//! Bundled factual game/executable reference. No network access or process enumeration.
use serde::Deserialize;
use std::sync::OnceLock;
#[derive(Deserialize)]
struct Catalog {
    games: Vec<Game>,
}
#[derive(Deserialize)]
struct Game {
    executables: Vec<String>,
}
pub fn executables() -> &'static [String] {
    static NAMES: OnceLock<Vec<String>> = OnceLock::new();
    NAMES.get_or_init(|| {
        let catalog: Catalog = serde_json::from_str(include_str!("game-catalog.json"))
            .expect("bundled game catalog must be valid");
        catalog
            .games
            .into_iter()
            .flat_map(|g| g.executables)
            .collect::<std::collections::BTreeSet<_>>()
            .into_iter()
            .collect()
    })
}
