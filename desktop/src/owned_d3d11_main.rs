fn main() {
    #[cfg(windows)]
    if std::env::args().any(|arg| arg == "--owned-d3d11-fixture") {
        if mnema_desktop_probe::owned_d3d11::native::run().is_err() {
            std::process::exit(1)
        }
        return;
    }
    eprintln!("Explicit Windows-only owned D3D11 fixture; no automatic GUI launch.");
    std::process::exit(2);
}
