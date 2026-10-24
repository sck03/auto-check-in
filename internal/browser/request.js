async (a) => {
  if (location.origin !== a.origin) return {status: 0, body: ''};
  const response = await fetch(a.path, {
    method: a.method, headers: a.headers, credentials: 'include', cache: 'no-store',
    redirect: 'error', body: a.body === null ? undefined : JSON.stringify(a.body),
  });
  const reader = response.body?.getReader();
  const decoder = new TextDecoder();
  const chunks = [];
  let bytes = 0;
  if (reader) {
    try {
      while (true) {
        const {done, value} = await reader.read();
        if (done) break;
        bytes += value.byteLength;
        if (bytes > a.limit) {
          await reader.cancel();
          throw Error('response exceeds 1 MiB');
        }
        chunks.push(decoder.decode(value, {stream: true}));
      }
    } finally {
      reader.releaseLock();
    }
  }
  return {status: response.status, body: chunks.join('') + decoder.decode(),
    retryAfter: response.headers.get('Retry-After') || ''};
}
