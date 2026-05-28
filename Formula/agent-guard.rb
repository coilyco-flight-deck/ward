class AgentGuard < Formula
  desc "Generic-purpose cli-guard consumer for repos with external contributors"
  homepage "https://forgejo.coilysiren.me/coilysiren/agent-guard"
  url "https://forgejo.coilysiren.me/coilysiren/agent-guard.git", tag: "v0.5.1", revision: "3cfcd761a617a8c3ebb3bd8247cf255597b27903"
  license "MIT"
  head "https://forgejo.coilysiren.me/coilysiren/agent-guard.git", branch: "main"

  depends_on "go" => :build

  def install
    # GOPROXY bypass for fresh cli-guard pseudo-versions. See docs/homebrew-build.md.
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
