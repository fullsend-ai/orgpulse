const entityIDPattern = /^[a-z0-9][a-z0-9-]*[a-z0-9]$|^[a-z0-9]$/

function canonicalEntityID(value: string): string {
  return value
    .trim()
    .normalize('NFKD')
    .replace(/\p{M}/gu, '')
    .toLowerCase()
}

export function entityIDError(
  value: string,
  existingIDs: Iterable<string>,
  label: string,
): string | null {
  const id = value.trim()
  if (!id) return `${label} is required.`
  if (!entityIDPattern.test(id))
    return `${label} must be lowercase letters, numbers, and hyphens only.`

  const canonicalID = canonicalEntityID(id)
  for (const existingID of existingIDs) {
    if (canonicalEntityID(existingID) === canonicalID)
      return `${label} "${id}" is already in use.`
  }
  return null
}
