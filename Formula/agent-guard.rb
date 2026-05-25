class AgentGuard < Formula
  desc "Generic-purpose cli-guard consumer for repos with external contributors"
  homepage "https://forgejo.coilysiren.me/coilysiren/agent-guard"
  url "https://forgejo.coilysiren.me/coilysiren/agent-guard.git", tag: "v0.1.2", revision: "98e695d0d0ac859b6ad08c8d579e11384bb8b57b"
  license "MIT"
  head "https://forgejo.coilysiren.me/coilysiren/agent-guard.git", branch: "main"

  depends_on "go" => :build

  def install
    # cli-guard has no semver tags yet, consumers pin via pseudo-version.
    # proxy.golang.org 403s the fresh pseudo-version on first fetch even
    # though the upstream github tarball is reachable. Bypass the proxy
    # for module fetches in the brew sandbox. See coilysiren/homebrew-tap#14.
    ENV["GOPROXY"] = "direct"
    ENV["GOSUMDB"] = "off"
    ldflags = "-s -w -X main.Version=v#{version}"
    system "go", "build", "-trimpath",
           "-ldflags", ldflags,
           "-o", bin/"agent-guard",
           "./cmd/agent-guard"
  end

  test do
    assert_match "v#{version}", shell_output("#{bin}/agent-guard version")
  end
end
