// gonano-school prepares small, domain-specialized training corpora for the
// gonano trainer from Kiwix ZIM archives.
//
// This module is intentionally dependency-free (standard library only). The
// training stage shells out to a gonano binary / source tree rather than
// importing it, so building this repository does not require GOEXPERIMENT=simd.
module gonano-school

go 1.27.1
