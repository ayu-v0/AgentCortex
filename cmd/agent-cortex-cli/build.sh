#!/usr/bin/env bash

set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$script_dir/../.." && pwd)"

binary_name="agent-cortex-cli"
target_os="${GOOS:-$(go env GOOS)}"
target_arch="${GOARCH:-$(go env GOARCH)}"
target_cgo="${CGO_ENABLED:-$(go env CGO_ENABLED)}"
output_dir="${OUTPUT_DIR:-$repo_root/dist}"
go_cache="${GOCACHE:-$repo_root/.gocache/build}"
go_tmp="${GOTMPDIR:-$repo_root/.gocache/tmp}"

version="${VERSION:-}"
if [[ -z "$version" ]] && command -v git >/dev/null 2>&1; then
    version="$(git -C "$repo_root" describe --tags --always --dirty 2>/dev/null || true)"
fi
version="${version:-dev}"
version="$(printf '%s' "$version" | sed 's/[^0-9A-Za-z._-]/-/g')"

executable_name="$binary_name"
if [[ "$target_os" == "windows" ]]; then
    executable_name+=".exe"
fi

package_name="${binary_name}_${version}_${target_os}_${target_arch}"
package_dir="$output_dir/$package_name"
archive_path="$output_dir/${package_name}.tar.gz"
checksum_path="${archive_path}.sha256"

rm -rf -- "$package_dir"
rm -f -- "$archive_path" "$checksum_path"
mkdir -p "$package_dir" "$go_cache" "$go_tmp"

echo "Building $binary_name ($target_os/$target_arch, CGO_ENABLED=$target_cgo)"
(
    cd "$repo_root"
    GOOS="$target_os" \
    GOARCH="$target_arch" \
    CGO_ENABLED="$target_cgo" \
    GOCACHE="$go_cache" \
    GOTMPDIR="$go_tmp" \
    TMPDIR="${TMPDIR:-$go_tmp}" \
    go build -trimpath -ldflags="-s -w" \
        -o "$package_dir/$executable_name" \
        "./cmd/$binary_name"
)

cp "$repo_root/README.md" "$repo_root/LICENSE" "$package_dir/"
tar -czf "$archive_path" -C "$output_dir" "$package_name"

archive_file="$(basename "$archive_path")"
(
    cd "$output_dir"
    if command -v sha256sum >/dev/null 2>&1; then
        sha256sum "$archive_file" > "$(basename "$checksum_path")"
    elif command -v shasum >/dev/null 2>&1; then
        shasum -a 256 "$archive_file" > "$(basename "$checksum_path")"
    elif command -v openssl >/dev/null 2>&1; then
        openssl dgst -sha256 -r "$archive_file" > "$(basename "$checksum_path")"
    else
        echo "No SHA-256 tool found (sha256sum, shasum, or openssl)." >&2
        exit 1
    fi
)

echo "Package:  $archive_path"
echo "Checksum: $checksum_path"
