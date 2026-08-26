import { lazyWithPreload } from './lazy-with-preload';
import { lazy, ComponentType } from 'react';
import type { LucideIcon } from 'lucide-react';
// 魔改：移除 Sparkles 图标 import（原 model/价格 导航项已删除）
import { Home, Radio, FolderTree, Logs, Settings } from 'lucide-react';

export type LazyComponent = ReturnType<typeof lazy> & {
    preload: () => Promise<{ default: ComponentType<Record<string, never>> }>
};

export interface RouteConfig {
    id: string;
    label: string;
    icon: LucideIcon;
    component: LazyComponent;
}

const Home_Module = lazyWithPreload(() => import('@/components/modules/home').then(m => ({ default: m.Home })));
const Channel_Module = lazyWithPreload(() => import('@/components/modules/channel').then(m => ({ default: m.Channel })));
const Group_Module = lazyWithPreload(() => import('@/components/modules/group').then(m => ({ default: m.Group })));
// 魔改：删除 model(界面显示为"价格")导航项，导航变 5 项：主页/渠道/分组/日志/设置
const Log_Module = lazyWithPreload(() => import('@/components/modules/log').then(m => ({ default: m.Log })));
const Setting_Module = lazyWithPreload(() => import('@/components/modules/setting').then(m => ({ default: m.Setting })));

export const ROUTES: RouteConfig[] = [
    { id: 'home', label: 'Home', icon: Home, component: Home_Module },
    { id: 'channel', label: 'Channel', icon: Radio, component: Channel_Module },
    { id: 'group', label: 'Group', icon: FolderTree, component: Group_Module },
    { id: 'log', label: 'Log', icon: Logs, component: Log_Module },
    { id: 'setting', label: 'Setting', icon: Settings, component: Setting_Module },
];

export const CONTENT_MAP = ROUTES.reduce((acc, route) => {
    acc[route.id] = route.component;
    return acc;
}, {} as Record<string, LazyComponent>);