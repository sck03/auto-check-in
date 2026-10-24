(origin, limit, maxEntries) => {
  if (location.origin !== origin) return {values: {}};
  if (localStorage.length > maxEntries) return {oversized: true};
  const encoder = new TextEncoder();
  const state = {};
  let bytes = 0;
  for (let i = 0; i < localStorage.length; i++) {
    const key = localStorage.key(i);
    const value = localStorage.getItem(key);
    bytes += encoder.encode(key).byteLength + encoder.encode(value).byteLength;
    if (bytes > limit) return {oversized: true};
    Object.defineProperty(state, key, {value, enumerable: true});
  }
  return {values: state};
}
