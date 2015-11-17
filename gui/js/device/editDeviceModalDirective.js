angular.module('syncthingfuse.device')
    .directive('editDeviceModal', function () {
        return {
            restrict: 'A',
            templateUrl: 'js/device/editDeviceModalView.html'
        };
});

angular.module('syncthingfuse.device')
    .directive('validDeviceid', function ($http) {
        return {
            require: 'ngModel',
            link: function (scope, elm, attrs, ctrl) {
                ctrl.$parsers.unshift(function (viewValue) {
                    if (scope.editingExisting) {
                        // we shouldn't validate
                        ctrl.$setValidity('validDeviceid', true);
                    } else {
                        $http.get('/api/verify/deviceid?id=' + viewValue).success(function (resp) {
                            if (resp.error) {
                                ctrl.$setValidity('validDeviceid', false);
                            } else {
                                ctrl.$setValidity('validDeviceid', true);
                            }
                        });
                    }
                    return viewValue;
                });
            }
        };
    });

angular.module('syncthingfuse.device')
    .directive('newDeviceid', function ($http) {
        return {
            require: 'ngModel',
            link: function (scope, elm, attrs, ctrl) {
                ctrl.$validators.newDeviceid = function (modelValue, viewValue) {
                    // true if device doesn't exist in config already

                    if (ctrl.$isEmpty(modelValue)) {
                        return false;
                    }

                    for (var i = 0; i < scope.config.devices.length; i++) {
                        var device = scope.config.devices[i];
                        if (device.deviceID === viewValue.toUpperCase()) {
                            return false
                        }
                    }

                    return true;
                }
            }
        };
    });