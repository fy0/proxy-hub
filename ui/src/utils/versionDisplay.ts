const SEMVER_DISPLAY_PATTERN =
  /^(?<prefix>v?)(?<core>\d+\.\d+\.\d+)(?<prerelease>-[0-9A-Za-z.-]+)?(?<build>\+[0-9A-Za-z.-]+)?$/;

export function formatVersionForDisplay(version: string): string {
  const trimmed = version.trim();
  if (!trimmed) {
    return version;
  }

  const matched = trimmed.match(SEMVER_DISPLAY_PATTERN);
  if (!matched?.groups) {
    return trimmed;
  }

  const { core, prerelease, build } = matched.groups;
  if (!build || prerelease) {
    return `${core}${prerelease ?? ''}${build ?? ''}`;
  }

  return core;
}
