#!/usr/bin/env ruby

require "json"
require "open3"
require "yaml"

def get(url, absent: ["404"])
  response, error, status = Open3.capture3(
    "curl", "--silent", "--show-error", "--location",
    "--connect-timeout", "10", "--max-time", "30",
    "--write-out", "\n%{http_code}", url,
  )
  raise "#{url}: #{error.strip}" unless status.success?

  body, _, code = response.rpartition("\n")
  return body if code == "200"
  return nil if absent.include?(code)

  raise "#{url}: HTTP #{code}"
end

def git_tag_commit(repo, tag)
  ref = "refs/tags/#{tag}"
  response, error, status = Open3.capture3(
    "git", "ls-remote", "--exit-code", "--tags", "https://github.com/#{repo}.git",
    ref, "#{ref}^{}",
  )
  return nil if status.exitstatus == 2
  raise "#{repo}: #{error.strip}" unless status.success?

  refs = response.lines.to_h { |line| line.split.reverse }
  raise "#{repo}: expected tag #{tag}" unless refs.key?(ref)

  commit = refs.fetch("#{ref}^{}", refs.fetch(ref))
  raise "#{repo}: invalid SHA for #{tag}" unless commit.match?(/\A[0-9a-f]{40}\z/)

  commit
end

def check_go(version)
  repo = "aldinokemal/gowa-sdk-go"
  tag = "v#{version}"
  major = version.split(".").first.to_i
  segment = major >= 2 ? "/v#{major}" : ""
  commit = git_tag_commit(repo, tag)
  if commit
    ["/GOWA-SOURCE.txt", "#{segment}/GOWA-SOURCE.txt"].uniq.each do |path|
      source = get("https://raw.githubusercontent.com/#{repo}/#{commit}#{path}")
      return "already published for OpenAPI #{version}" if source&.lines&.map(&:strip)&.include?("OpenAPI version: #{version}")
    end
    raise "tag #{tag} is occupied by an SDK without matching OpenAPI provenance"
  end

  proxy_url = "https://proxy.golang.org/github.com/#{repo}#{segment}/@v/#{tag}.info"
  raise "#{tag} is already cached by the Go module proxy without a matching Git tag" if get(proxy_url, absent: ["404", "410"])

  "tag #{tag} is available"
end

def check_node(version)
  url = "https://registry.npmjs.org/@aldinokemal2104%2fgowa-sdk-node/#{version}"
  body = get(url)
  return "package version #{version} is available" unless body

  manifest = JSON.parse(body)
  if manifest["version"] == version && manifest.dig("gowaSource", "openapiVersion") == version
    return "already published for OpenAPI #{version}"
  end

  raise "npm version #{version} is occupied by an SDK without matching OpenAPI provenance"
end

def check_php(version)
  repo = "aldinokemal/gowa-sdk-php"
  tag = "v#{version}"
  commit = git_tag_commit(repo, tag)
  index = JSON.parse(get("https://repo.packagist.org/p2/#{repo}.json", absent: []))
  published = index.fetch("packages").fetch(repo).find { |entry| entry.fetch("version").delete_prefix("v") == version }
  if published && published.dig("source", "reference") != commit
    raise "Composer version #{version} is indexed without a matching SDK Git tag"
  end
  return "tag #{tag} is available" unless commit

  message = JSON.parse(get("https://api.github.com/repos/#{repo}/git/commits/#{commit}", absent: [])).fetch("message")
  return "already tagged for OpenAPI #{version}" if message.lines.map(&:strip).include?("GOWA-OpenAPI-Version: #{version}")

  raise "tag #{tag} is occupied by an SDK without matching OpenAPI provenance"
end

abort "Usage: ruby scripts/check-sdk-version.rb [openapi.yaml]" if ARGV.length > 1
path = ARGV.fetch(0, File.expand_path("../docs/openapi.yaml", __dir__))
version = YAML.safe_load(File.read(path), aliases: true).dig("info", "version")
unless version.is_a?(String) && version.match?(/\A(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\z/)
  abort "Expected a stable semver in OpenAPI info.version (for example, 9.7.0)"
end

puts "Checking SDK version slots for OpenAPI #{version}"
results = {"Go" => method(:check_go), "Node" => method(:check_node), "PHP" => method(:check_php)}.map do |sdk, check|
  begin
    puts "#{sdk}: #{check.call(version)}"
    true
  rescue RuntimeError, JSON::ParserError, KeyError => error
    warn "#{sdk}: #{error.message}"
    false
  end
end
abort "SDK version preflight failed for OpenAPI #{version}. Resolve occupied versions or failed lookups before creating the GOWA tag." unless results.all?
