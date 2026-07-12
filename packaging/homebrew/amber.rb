# Homebrew formula template for a manual tap.
#
# goreleaser regenerates this automatically on release (see .goreleaser.yaml
# `brews:`), filling in the real version, URLs, and sha256s. This checked-in
# copy documents the tap and lets `brew install --build-from-source` work
# before the first release.
#
#   brew install ghostlygawd/tap/amber
class Amber < Formula
  desc "Local-first, long-term memory for AI coding agents"
  homepage "https://github.com/ghostlygawd/amber"
  license "MIT"
  version "0.1.0"

  # Release archives (filled by goreleaser). Placeholders until first tag.
  on_macos do
    on_arm do
      url "https://github.com/ghostlygawd/amber/releases/download/v0.1.0/amber_0.1.0_darwin_arm64.tar.gz"
      sha256 "0000000000000000000000000000000000000000000000000000000000000000"
    end
    on_intel do
      url "https://github.com/ghostlygawd/amber/releases/download/v0.1.0/amber_0.1.0_darwin_amd64.tar.gz"
      sha256 "0000000000000000000000000000000000000000000000000000000000000000"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/ghostlygawd/amber/releases/download/v0.1.0/amber_0.1.0_linux_arm64.tar.gz"
      sha256 "0000000000000000000000000000000000000000000000000000000000000000"
    end
    on_intel do
      url "https://github.com/ghostlygawd/amber/releases/download/v0.1.0/amber_0.1.0_linux_amd64.tar.gz"
      sha256 "0000000000000000000000000000000000000000000000000000000000000000"
    end
  end

  # Build-from-source fallback.
  head "https://github.com/ghostlygawd/amber.git", branch: "main"

  def install
    if build.head?
      system "go", "build", *std_go_args(ldflags: "-s -w", output: bin/"amber"), "./cmd/amber"
    else
      bin.install "amber"
    end
  end

  def caveats
    <<~EOS
      Run `amber init` to create your store.
      Semantic recall wants a one-time ~30MB local model:
        amber doctor --fetch-model
    EOS
  end

  test do
    assert_match "amber", shell_output("#{bin}/amber --version")
    system bin/"amber", "init", "--defaults"
  end
end
