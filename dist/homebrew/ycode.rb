# frozen_string_literal: true

# Homebrew formula for ycode.
#
# This file is the canonical source. The Homebrew tap repo
# (qiangli/homebrew-ycode) holds a copy of this file under Formula/ycode.rb,
# kept in sync by .github/workflows/update-homebrew-tap.yml on every release.
#
# To install once the tap is published:
#   brew tap qiangli/ycode
#   brew install ycode

class Ycode < Formula
  desc "Pure Go CLI agent harness for autonomous software development"
  homepage "https://github.com/qiangli/ycode"
  version "0.1.0"
  license "MIT"

  on_macos do
    on_arm do
      url "https://github.com/qiangli/ycode/releases/download/v#{version}/ycode-darwin-arm64.tar.gz"
      sha256 "61ee922694c816854be77ccd565ae6e3811bc154fb86578ec1f7295de78c3e8d"
    end
    # darwin-amd64 not yet packaged — tracked in release.yml matrix.
  end

  on_linux do
    on_intel do
      url "https://github.com/qiangli/ycode/releases/download/v#{version}/ycode-linux-amd64.tar.gz"
      sha256 "6cfecb339fa39badb6b67411ea1d24ea8db9fe64dcc40c169544203edce0aa29"
    end
    # linux-arm64 not yet packaged — tracked in release.yml matrix.
  end

  def install
    bin.install "ycode"
  end

  test do
    output = shell_output("#{bin}/ycode version")
    assert_match "ycode #{version}", output
  end
end
