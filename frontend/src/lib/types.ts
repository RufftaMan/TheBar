export interface KnownApp {
  name: string;
  nickname?: string;
  port?: number;
  scheme?: string;
  helper?: boolean;
  helperOf?: string;
}

export interface Port {
  host: number;
  container: number;
  proto: string;
}

export interface Container {
  id: string;
  name: string;
  project?: string;
  service?: string;
  image: string;
  imageId: string;
  state: string;
  status: string;
  health?: string;
  created: string;
  startedAt?: string;
  finishedAt?: string;
  exitCode: number;
  oomKilled?: boolean;
  restartCount: number;
  restartPolicy?: string;
  networkMode: string;
  ports: Port[];
  exposed?: number[];
  dependsOn?: string[];
  configFiles?: string[];
  workingDir?: string;
  labels?: Record<string, string>;
  self?: boolean;
  webPort?: number;
  webScheme?: string;
  appKey?: string;
  app?: KnownApp;
  icon?: string;
  domains?: string[];
  update?: '' | 'available' | 'recreate' | 'current' | 'local';
  drift?: boolean;
  sizeRw?: number;
  volSize?: number;
  alert?: string;
}

export interface Sample {
  t: number;
  cpu: number;
  mem: number;
  limit: number;
  rx: number;
  tx: number;
  br: number;
  bw: number;
  net: boolean;
}

export interface Disk {
  label: string;
  path: string;
  total: number;
  used: number;
  problem?: string; // set when the disk can't actually be read
}

export interface HostInfo {
  hostname: string;
  cpu: number;
  cores: number;
  load: [number, number, number];
  memTotal: number;
  memUsed: number;
  uptime: number;
  iface: string;
  rx: number;
  tx: number;
  disks: Disk[] | null;
}

export interface Job {
  id: string;
  title: string;
  state: 'running' | 'ok' | 'failed';
  lines: string[];
  started: number;
  ended?: number;
}

// ---- layout (persisted on the server, shared between browsers) ----

export interface CardPrefs {
  nickname?: string; // undefined = use suggestion, "" = none
  note?: string;
  url?: string;
  icon?: string; // dashboard-icons slug or full URL
  hidden?: boolean;
  parent?: string; // undefined = automatic, "" = always its own card, key = fold into that card
}

export interface GroupPrefs {
  name?: string;
  collapsed?: boolean;
  members?: string[];
}

export interface Settings {
  preferDomain: boolean;
  linkHost: string;
  grafana: string;
}

export interface Layout {
  v: 1;
  top: string[]; // "g:<groupId>" | "c:<cardKey>"
  groups: Record<string, GroupPrefs>;
  cards: Record<string, CardPrefs>;
  dissolved: string[]; // projects whose automatic group was removed
  seen: string[];
  settings: Settings;
}

// ---- derived view ----

export type Level = 'good' | 'warning' | 'critical' | 'stopped';

export interface CardStatus {
  level: Level;
  label: string;
  running: number;
  total: number;
}

export interface LinkInfo {
  label: string;
  url: string;
}

export interface CardView {
  key: string;
  title: string; // big name: nickname or app name
  subtitle: string; // smaller: real name
  appName: string;
  own: Container[];
  folded: { key: string; title: string; containers: Container[] }[];
  all: Container[];
  main: Container;
  project?: string;
  service?: string;
  url: string | null;
  links: LinkInfo[];
  icon: string;
  status: CardStatus;
  isNew: boolean;
  hidden: boolean;
  series: number; // >0 for a series card (e.g. 20 similar one-off containers)
  update: boolean;
  recreate: boolean;
  drift: boolean;
  note: string;
  groupId?: string;
  search: string;
}

export interface GroupView {
  id: string;
  name: string;
  project?: string; // set for automatic groups from a compose project
  cards: CardView[];
  collapsed: boolean;
  status: CardStatus;
}

export type TopItem = { kind: 'card'; id: string; card: CardView } | { kind: 'group'; id: string; group: GroupView };
