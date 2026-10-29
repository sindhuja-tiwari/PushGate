//! Per-release limits. Each build enables exactly one `vNN` feature.
//! The table is mirrored in docs/versions.md; keep them in sync.
//!
//! | version | MAX_FEATURES | INPUT_SLOTS |
//! |---------|--------------|-------------|
//! | v41     | 128          | 20          |
//! | v42     | 200          | 20          |
//! | v43     | 200          | 20          |
//! | v44     | 200          | 21          |

#[cfg(feature = "v41")]
mod v {
    pub const BUILD_VERSION: u32 = 41;
    pub const MAX_FEATURES: usize = 128;
    pub const INPUT_SLOTS: usize = 20;
}

#[cfg(feature = "v42")]
mod v {
    pub const BUILD_VERSION: u32 = 42;
    pub const MAX_FEATURES: usize = 200;
    pub const INPUT_SLOTS: usize = 20;
}

#[cfg(feature = "v43")]
mod v {
    pub const BUILD_VERSION: u32 = 43;
    pub const MAX_FEATURES: usize = 200;
    pub const INPUT_SLOTS: usize = 20;
}

#[cfg(feature = "v44")]
mod v {
    pub const BUILD_VERSION: u32 = 44;
    pub const MAX_FEATURES: usize = 200;
    pub const INPUT_SLOTS: usize = 21;
}

// A plain `cargo build` or `cargo test` with no version feature acts as v44.
#[cfg(not(any(feature = "v41", feature = "v42", feature = "v43", feature = "v44")))]
mod v {
    pub const BUILD_VERSION: u32 = 44;
    pub const MAX_FEATURES: usize = 200;
    pub const INPUT_SLOTS: usize = 21;
}

#[cfg(any(
    all(feature = "v41", feature = "v42"),
    all(feature = "v41", feature = "v43"),
    all(feature = "v41", feature = "v44"),
    all(feature = "v42", feature = "v43"),
    all(feature = "v42", feature = "v44"),
    all(feature = "v43", feature = "v44"),
))]
compile_error!("enable exactly one version feature (v41, v42, v43 or v44)");

pub use v::*;
