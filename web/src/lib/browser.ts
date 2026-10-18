// Small browser effects kept out of components so they can be mocked in tests.

export async function copyText(text: string): Promise<void> {
  await navigator.clipboard.writeText(text);
}

/** Saves text as a file through a Blob and a temporary <a download> (§5.6.6). */
export function downloadText(fileName: string, text: string): void {
  const url = URL.createObjectURL(new Blob([text], { type: "text/plain;charset=utf-8" }));
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = fileName;
  document.body.append(anchor);
  anchor.click();
  anchor.remove();
  URL.revokeObjectURL(url);
}
