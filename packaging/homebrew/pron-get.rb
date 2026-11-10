# Homebrew formula for the pron-get/homebrew-tap repository
# (install with: brew install pron-get/tap/pron-get).
#
# On each release, copy this file to the tap as Formula/pron-get.rb and set
# url and sha256 to the release's source tarball:
#   curl -sL <url> | shasum -a 256
class PronGet < Formula
  desc "Apt-style package manager for porn, over Tor"
  homepage "https://github.com/pron-get/pron-get"
  url "https://github.com/pron-get/pron-get/archive/refs/tags/v0.7.5.tar.gz"
  sha256 "REPLACE_WITH_RELEASE_TARBALL_SHA256"
  license "GPL-2.0-or-later"

  depends_on "go" => :build
  depends_on "tor"
  depends_on "ffmpeg" => :recommended

  def install
    system "go", "build", *std_go_args(ldflags: "-X main.version=#{version}"), "./cmd/pron-get"
    bin.install_symlink "pron-get" => "pron-cache"
    man1.install "man/pron-get.1"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/pron-get version")
    assert_match "Super Cow Powers", shell_output("#{bin}/pron-get moo; #{bin}/pron-get help")
  end
end
