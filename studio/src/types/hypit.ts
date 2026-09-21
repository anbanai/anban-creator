export interface HypitAsset { type: 'image' | 'video' | 'video_url' | 'audio'; url?: string; task_file_id?: string; file_name?: string; mime_type?: string; file_size?: number }
export interface HypitPreferences { duration_seconds?: number; aspect_ratio?: 'source' | '9:16' | '16:9' | '1:1'; language?: string }
export interface HypitInput { brief: string; reference?: HypitAsset; source_assets?: HypitAsset[]; preferences?: HypitPreferences }
export interface HypitDefaults { preferences?: HypitPreferences; asset_guidance?: string }
export interface HypitLimits { max_duration_seconds: number; max_assets: number; max_asset_bytes: number; max_input_bytes: number; max_project_bytes?: number; max_expanded_bytes?: number; max_project_files?: number; max_video_bytes?: number; timeout_minutes?: number }
export interface HypitCapabilities { enabled: boolean; configured: boolean; missing_configuration: string[]; limits: HypitLimits }
