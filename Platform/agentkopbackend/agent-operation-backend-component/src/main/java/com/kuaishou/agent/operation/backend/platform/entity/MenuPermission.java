package com.kuaishou.agent.operation.backend.platform.entity;

import com.baomidou.mybatisplus.annotation.IdType;
import com.baomidou.mybatisplus.annotation.TableId;
import com.baomidou.mybatisplus.annotation.TableName;

import lombok.Data;

/**
 * 菜单权限实体（aop_menu_permission）。
 */
@Data
@TableName("aop_menu_permission")
public class MenuPermission {
    @TableId(value = "id", type = IdType.AUTO)
    private Integer id;
    /** 父节点 ID：根节点为 0。 */
    private Integer parentId;
    /** 层级：1/2/3。 */
    private Integer level;
    private String menuName;
    private String menuUrl;
}
