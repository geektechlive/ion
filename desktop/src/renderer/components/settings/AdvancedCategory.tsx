import React from 'react'
import { SettingHeading } from './SettingHeading'
import { PresetsCategory } from './PresetsCategory'
import { MigrationCategory } from './MigrationCategory'
import { BackupRestoreCategory } from './BackupRestoreCategory'
import { DeveloperCategory } from './DeveloperCategory'

export function AdvancedCategory() {
  return (
    <>
      <SettingHeading first>Presets</SettingHeading>
      <PresetsCategory />
      <MigrationCategory />
      <BackupRestoreCategory />
      <DeveloperCategory />
    </>
  )
}
