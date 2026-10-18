// File-tree building for the Files tab (docs/frontend/revision2.md §5.6.5).

export interface FileEntry {
  id: string;
  path: string;
  sizeBytes: number;
}

export type FileTreeNode =
  | { kind: "file"; id: string; name: string; path: string; sizeBytes: number }
  | {
      kind: "folder";
      name: string;
      path: string;
      sizeBytes: number;
      children: FileTreeNode[];
      defaultExpanded: boolean;
    };

interface FolderNode {
  name: string;
  path: string;
  children: Map<string, FolderNode>;
  files: FileTreeNode[];
}

const collator = new Intl.Collator(undefined, { numeric: true });

function compareNames(a: string, b: string): number {
  return collator.compare(a, b);
}

function newFolder(name: string, parentPath: string): FolderNode {
  const path = parentPath === "" ? name : `${parentPath}/${name}`;
  return { name, path, children: new Map(), files: [] };
}

const EXPAND_ALL_THRESHOLD = 50;

/**
 * Builds a sorted, size-summed tree from `files[].path`, folders first, with
 * single-child folder chains collapsed and the ≤50-file default expansion rule.
 */
export function buildFileTree(files: FileEntry[]): FileTreeNode[] {
  const root = newFolder("", "");
  for (const file of files) {
    const segments = file.path.split("/").filter((segment) => segment.length > 0);
    if (segments.length === 0) {
      continue;
    }
    const name = segments[segments.length - 1] ?? file.path;
    let node = root;
    for (let i = 0; i < segments.length - 1; i += 1) {
      const segment = segments[i];
      if (segment === undefined) {
        continue;
      }
      let child = node.children.get(segment);
      if (child === undefined) {
        child = newFolder(segment, node.path);
        node.children.set(segment, child);
      }
      node = child;
    }
    node.files.push({
      kind: "file",
      id: file.id,
      name,
      path: file.path,
      sizeBytes: file.sizeBytes,
    });
  }
  const expandAll = files.length <= EXPAND_ALL_THRESHOLD;
  const children: FileTreeNode[] = [];
  for (const folder of [...root.children.values()].sort((a, b) => compareNames(a.name, b.name))) {
    children.push(buildFolder(folder, expandAll, 0));
  }
  const sortedRootFiles = root.files.sort((a, b) => compareNames(a.name, b.name));
  return collapseSingleChildFolders([...children, ...sortedRootFiles]);
}

function buildFolder(node: FolderNode, expandAll: boolean, depth: number): FileTreeNode {
  const children: FileTreeNode[] = [];
  for (const folder of [...node.children.values()].sort((a, b) => compareNames(a.name, b.name))) {
    children.push(buildFolder(folder, expandAll, depth + 1));
  }
  for (const file of node.files.sort((a, b) => compareNames(a.name, b.name))) {
    children.push(file);
  }
  const sizeBytes = children.reduce((sum, child) => sum + child.sizeBytes, 0);
  return {
    kind: "folder",
    name: node.name,
    path: node.path,
    sizeBytes,
    children,
    defaultExpanded: expandAll || depth === 0,
  };
}

export type FileTreeFolder = Extract<FileTreeNode, { kind: "folder" }>;

/** Collapses a chain of folders with a single folder child into one row ("Sintel/Extras"). */
export function collapseSingleChildFolders(root: FileTreeNode[]): FileTreeNode[] {
  return root.map(collapseNode);
}

function collapseNode(node: FileTreeNode): FileTreeNode {
  if (node.kind === "file") {
    return node;
  }
  let current: FileTreeFolder = {
    ...node,
    children: node.children.map(collapseNode),
  };
  while (current.children.length === 1) {
    const child = current.children[0];
    if (child === undefined || child.kind !== "folder") {
      break;
    }
    current = {
      ...current,
      name: `${current.name}/${child.name}`,
      path: child.path,
      children: child.children,
    };
  }
  return current;
}
