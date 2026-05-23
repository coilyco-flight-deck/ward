class AgentGuard < Formula
  desc "Generic-purpose cli-guard consumer for repos with external contributors"
  homepage "https://github.com/coilysiren/agent-guard"
  url "ssh://git@github.com/coilysiren/agent-guard.git", tag: "v0.1.1", revision: "b0850148fe4d1565d5bdb4eb236762551d9c3a71"
  license "MIT"
  head "https://github.com/coilysiren/agent-guard.git", branch: "main"

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
