import { describe, expect, it } from "vitest";
import { buildFileTree, collapseSingleChildFolders, type FileTreeNode } from "@/lib/filetree";

function names(nodes: FileTreeNode[]): string[] {
  return nodes.map((node) => node.name);
}

describe("buildFileTree", () => {
  it("sorts folders before files, then by name", () => {
    const tree = buildFileTree([
      { id: "1", path: "b.txt", sizeBytes: 1 },
      { id: "2", path: "a.txt", sizeBytes: 1 },
      { id: "3", path: "Dir/z.txt", sizeBytes: 1 },
    ]);
    expect(names(tree)).toEqual(["Dir", "a.txt", "b.txt"]);
  });

  it("sorts numeric names with Intl.Collator numeric", () => {
    const tree = buildFileTree([
      { id: "1", path: "Episode 10.mp4", sizeBytes: 1 },
      { id: "2", path: "Episode 2.mp4", sizeBytes: 1 },
      { id: "3", path: "Episode 1.mp4", sizeBytes: 1 },
    ]);
    expect(names(tree)).toEqual(["Episode 1.mp4", "Episode 2.mp4", "Episode 10.mp4"]);
  });

  it("sums the sizes of files below a folder", () => {
    const tree = buildFileTree([
      { id: "1", path: "Dir/a.txt", sizeBytes: 100 },
      { id: "2", path: "Dir/b.txt", sizeBytes: 200 },
    ]);
    expect(tree).toHaveLength(1);
    expect(tree[0]).toMatchObject({ kind: "folder", sizeBytes: 300 });
  });

  it("collapses single-child folder chains", () => {
    const tree = buildFileTree([{ id: "1", path: "Sintel/Extras/poster.jpg", sizeBytes: 5 }]);
    expect(names(tree)).toEqual(["Sintel/Extras"]);
  });

  it("does not collapse a folder with a single file child", () => {
    const tree = buildFileTree([{ id: "1", path: "Dir/only.txt", sizeBytes: 5 }]);
    expect(names(tree)).toEqual(["Dir"]);
    const folder = tree[0];
    if (folder && folder.kind === "folder") {
      expect(names(folder.children)).toEqual(["only.txt"]);
    }
  });

  it("expands every folder for ≤50 files", () => {
    const tree = buildFileTree([
      { id: "1", path: "a/b/c.txt", sizeBytes: 1 },
      { id: "2", path: "a/d/e.txt", sizeBytes: 1 },
    ]);
    const a = tree[0];
    expect(a?.kind).toBe("folder");
    if (a?.kind === "folder") {
      expect(a.defaultExpanded).toBe(true);
      expect(a.children[0]?.kind === "folder" && a.children[0].defaultExpanded).toBe(true);
    }
  });

  it("expands only the first level for >50 files", () => {
    const files = Array.from({ length: 51 }, (_, i) => ({
      id: `f${i}`,
      path: `a/f${i}.txt`,
      sizeBytes: 1,
    }));
    files.push({ id: "nested", path: "a/b/c.txt", sizeBytes: 1 });
    const tree = buildFileTree(files);
    const a = tree[0];
    expect(a?.kind).toBe("folder");
    if (a?.kind === "folder") {
      expect(a.defaultExpanded).toBe(true);
      const b = a.children.find((child) => child.kind === "folder" && child.name === "b");
      expect(b?.kind === "folder" && b.defaultExpanded).toBe(false);
    }
  });

  it("handles 10 000 files", () => {
    const files = Array.from({ length: 10_000 }, (_, i) => ({
      id: String(i),
      path: `Folder/file-${i}.bin`,
      sizeBytes: 1,
    }));
    const tree = buildFileTree(files);
    expect(tree).toHaveLength(1);
    const folder = tree[0];
    expect(folder?.kind).toBe("folder");
    if (folder?.kind === "folder") {
      expect(folder.children).toHaveLength(10_000);
      expect(folder.sizeBytes).toBe(10_000);
    }
  });
});

describe("collapseSingleChildFolders", () => {
  it("merges a full chain of single-child folders", () => {
    const nodes = collapseSingleChildFolders(
      buildFileTree([{ id: "1", path: "A/B/C/file.txt", sizeBytes: 1 }]),
    );
    expect(names(nodes)).toEqual(["A/B/C"]);
  });
});
