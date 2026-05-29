class Ward < Formula
  desc "coilysiren's contributor-facing cli-guard consumer"
  homepage "https://forgejo.coilysiren.me/coilysiren/agent-guard"
  url "https://forgejo.coilysiren.me/coilyco-flight-deck/ward.git", tag: "v0.5.6", revision: "75e5d9b4b3330d978f17786f36292a513e5f72ec"
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
           "-o", bin/"ward",
           "./cmd/ward"
  end

  test do
    assert_match "v#{version}", shell_output("#{bin}/ward version")
  end
end
