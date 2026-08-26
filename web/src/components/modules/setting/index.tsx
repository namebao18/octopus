'use client';

// 设置页组装模块。
// 魔改说明：相比上游 v0.9.28 移除了「LLM 价格」面板（SettingLLMPrice），
// 价格数据仅用于成本展示，本部署不需要该界面。

import { PageWrapper } from '@/components/common/PageWrapper';
import { SettingAppearance } from './Appearance';
import { SettingSystem } from './System';
import { SettingAPIKey } from './APIKey';
import { SettingAccount } from './Account';
import { SettingInfo } from './Info';
import { SettingLLMSync } from './LLMSync';
import { SettingLog } from './Log';
import { SettingBackup } from './Backup';
import { SettingCircuitBreaker } from './CircuitBreaker';

export function Setting() {
    return (
        <div className="h-full min-h-0 overflow-y-auto overscroll-contain rounded-t-3xl">
            <PageWrapper className="columns-1 gap-4 pb-24 md:columns-2 md:pb-4 *:mb-4 *:break-inside-avoid">
                <SettingInfo key="setting-info" />
                <SettingAppearance key="setting-appearance" />
                <SettingAccount key="setting-account" />
                <SettingSystem key="setting-system" />
                <SettingLog key="setting-log" />
                <SettingAPIKey key="setting-apikey" />
                <SettingLLMSync key="setting-llmsync" />
                <SettingCircuitBreaker key="setting-circuit-breaker" />
                <SettingBackup key="setting-backup" />
            </PageWrapper>
        </div>
    );
}
