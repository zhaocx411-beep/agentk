package com.kuaishou.agent.operation.backend.controller;

import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestBody;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RequestParam;
import org.springframework.web.bind.annotation.RestController;

import com.baomidou.mybatisplus.extension.plugins.pagination.Page;
import com.kuaishou.agent.operation.backend.model.Message;
import com.kuaishou.agent.operation.backend.model.ResultCode;
import com.kuaishou.agent.operation.backend.platform.dto.PageResult;
import com.kuaishou.agent.operation.backend.platform.entity.WhiteListUser;
import com.kuaishou.agent.operation.backend.platform.service.WhiteListUserService;

/**
 * 菜单白名单用户管理控制器（复用 aop_platform_admin 存储授权）。
 */
@RestController
@RequestMapping("/agent/operation/backend/platform/whiteUser")
public class WhiteListUserController {
    private static final Logger logger = LoggerFactory.getLogger(WhiteListUserController.class);

    private final WhiteListUserService whiteListUserService;

    public WhiteListUserController(WhiteListUserService whiteListUserService) {
        this.whiteListUserService = whiteListUserService;
    }

    @PostMapping("/add")
    public Message<WhiteListUser> addWhiteListUser(@RequestBody WhiteListUser request) {
        try {
            logger.info("新增白名单用户: username={}", request == null ? null : request.getUsername());
            return Message.ok(whiteListUserService.addValidated(request));
        } catch (IllegalArgumentException e) {
            logger.warn("新增白名单用户参数错误: {}", e.getMessage());
            return Message.error(ResultCode.PARAM_VALIDATION_FAILURE, "参数错误：" + e.getMessage());
        } catch (Exception e) {
            logger.error("新增白名单用户失败", e);
            return Message.error(ResultCode.SERVICE_BUSY, "新增失败：" + e.getMessage());
        }
    }

    @PostMapping("/update")
    public Message<WhiteListUser> updateWhiteListUser(@RequestBody WhiteListUser request) {
        try {
            logger.info("更新白名单用户: id={}", request == null ? null : request.getId());
            WhiteListUser updated = whiteListUserService.updateValidated(request);
            if (updated == null) {
                return Message.error(ResultCode.NOT_FOUND, "用户不存在");
            }
            return Message.ok(updated);
        } catch (IllegalArgumentException e) {
            logger.warn("更新白名单用户参数错误: {}", e.getMessage());
            return Message.error(ResultCode.PARAM_VALIDATION_FAILURE, "参数错误：" + e.getMessage());
        } catch (Exception e) {
            logger.error("更新白名单用户失败", e);
            return Message.error(ResultCode.SERVICE_BUSY, "更新失败：" + e.getMessage());
        }
    }

    @PostMapping("/delete")
    public Message<String> deleteWhiteListUsers(@RequestParam String ids) {
        try {
            int deleted = whiteListUserService.deleteWhiteListUsers(whiteListUserService.parseIds(ids));
            return Message.ok(String.format("成功删除 %d 条记录", deleted));
        } catch (IllegalArgumentException e) {
            logger.warn("删除白名单用户参数错误: {}", e.getMessage());
            return Message.error(ResultCode.PARAM_VALIDATION_FAILURE, "参数错误：" + e.getMessage());
        } catch (Exception e) {
            logger.error("删除白名单用户失败", e);
            return Message.error(ResultCode.SERVICE_BUSY, "删除失败：" + e.getMessage());
        }
    }

    @PostMapping("/get")
    public Message<WhiteListUser> getWhiteListUserById(@RequestParam Long id) {
        try {
            WhiteListUser user = whiteListUserService.getByIdWithMenuNames(id);
            if (user == null) {
                return Message.error(ResultCode.NOT_FOUND, "用户不存在");
            }
            return Message.ok(user);
        } catch (IllegalArgumentException e) {
            logger.warn("查询白名单用户参数错误: {}", e.getMessage());
            return Message.error(ResultCode.PARAM_VALIDATION_FAILURE, "参数错误：" + e.getMessage());
        } catch (Exception e) {
            logger.error("查询白名单用户失败", e);
            return Message.error(ResultCode.SERVICE_BUSY, "查询失败：" + e.getMessage());
        }
    }

    @PostMapping("/list")
    public Message<PageResult<WhiteListUser>> listWhiteListUsers(
            @RequestParam(required = false) String username,
            @RequestParam(required = false) Integer status,
            @RequestParam(defaultValue = "1") Integer pageNum,
            @RequestParam(defaultValue = "10") Integer pageSize) {
        try {
            Page<WhiteListUser> page = whiteListUserService.listWhiteListUsers(username, status, pageNum, pageSize);
            return Message.ok(PageResult.of(page.getCurrent(), page.getSize(), page.getTotal(),
                    page.getRecords() == null ? java.util.Collections.emptyList() : page.getRecords()));
        } catch (IllegalArgumentException e) {
            logger.warn("分页查询白名单用户参数错误: {}", e.getMessage());
            return Message.error(ResultCode.PARAM_VALIDATION_FAILURE, "参数错误：" + e.getMessage());
        } catch (Exception e) {
            logger.error("分页查询白名单用户失败", e);
            return Message.error(ResultCode.SERVICE_BUSY, "查询失败：" + e.getMessage());
        }
    }
}
