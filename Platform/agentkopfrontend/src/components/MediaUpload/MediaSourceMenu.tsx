import { Button, Dropdown, type ButtonProps, type MenuProps } from 'antd';
import {
  CloudOutlined,
  FolderOpenOutlined,
  HistoryOutlined,
  PlusOutlined,
  UploadOutlined,
} from '@ant-design/icons';
import type { MediaPickerTab } from './types';

interface MediaSourceMenuProps {
  disabled?: boolean;
  pickerDisabled?: boolean;
  showLabel?: boolean;
  label?: string;
  buttonType?: ButtonProps['type'];
  buttonSize?: ButtonProps['size'];
  buttonShape?: ButtonProps['shape'];
  className?: string;
  onLocalUpload: () => void;
  onSelectSource: (tab: MediaPickerTab) => void;
}

function MediaSourceMenu({
  disabled = false,
  pickerDisabled = false,
  showLabel = true,
  label = '添加素材',
  buttonType = 'default',
  buttonSize,
  buttonShape,
  className,
  onLocalUpload,
  onSelectSource,
}: MediaSourceMenuProps) {
  const items: MenuProps['items'] = [
    { key: 'local', icon: <UploadOutlined />, label: '本地上传' },
    { key: 'portfolio', icon: <HistoryOutlined />, label: '历史创作', disabled: pickerDisabled },
    { key: 'uploads', icon: <CloudOutlined />, label: '历史上传', disabled: pickerDisabled },
    { key: 'material', icon: <FolderOpenOutlined />, label: '素材库', disabled: pickerDisabled },
  ];

  const handleClick: MenuProps['onClick'] = ({ key }) => {
    if (key === 'local') {
      onLocalUpload();
      return;
    }
    if (key === 'portfolio' || key === 'uploads' || key === 'material') {
      onSelectSource(key);
    }
  };

  return (
    <Dropdown
      menu={{ items, onClick: handleClick }}
      trigger={['click']}
      placement="topLeft"
      disabled={disabled}
    >
      <Button
        type={buttonType}
        size={buttonSize}
        shape={buttonShape}
        icon={<PlusOutlined />}
        className={className}
        disabled={disabled}
        aria-label={label}
        title={label}
      >
        {showLabel ? label : null}
      </Button>
    </Dropdown>
  );
}

export { MediaSourceMenu };
